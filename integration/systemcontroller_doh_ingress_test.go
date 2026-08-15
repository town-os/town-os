// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/caddysup"
	"gitea.com/town-os/town-os/src/ingress"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// TestDohResolvesThroughTheIngress is the end-to-end proof for the DoH vhost:
// a real rolodex serving DoH behind a real caddy ingress, queried by a real
// RFC 8484 client over TLS the client actually validates.
//
// Nothing cheaper reaches this. dohIngressRoute is unit-tested for its fields;
// TestIngressProxiesToAnHTTPSBackend proves caddy can proxy an HTTPS hop, but
// against an httptest server standing in for rolodex. Neither can catch the
// things that only exist where the two meet: rolodex refusing the `doh:` config
// key, its listener never binding, the DoH router 404ing the path the ingress
// forwards, or a TLS handshake the proxy hop cannot complete against a
// self-signed peer. Each of those is a 502 or a timeout that reads identically
// from the client, and each would ship green.
//
// The certificate story is the whole point of the vhost. rolodex terminates its
// own TLS with a self-signed certificate that no validating DoH client accepts;
// the ingress holds a leaf from the box's CA for this name and skips
// verification on the internal hop. So this test validates the ingress's leaf
// against the CA — a client with RootCAs set and no InsecureSkipVerify — which
// is exactly what makes the arrangement worth having.
func TestDohResolvesThroughTheIngress(t *testing.T) {
	t.Parallel()

	caddyBin := findCaddy(t)

	// Every listener gets an ephemeral port. The production DoH backend is a
	// fixed 127.0.0.2:4443 (systemcontroller.RolodexDohBackend) and is
	// deliberately NOT used here: two concurrent test-full runs would collide on
	// it, and one of them would be scraping the other's resolver. IRON RULE.
	dnsPort := findFreePort(t)
	dohPort := findFreePort(t)
	dohBackend := net.JoinHostPort(rolodex.DNSLoopback, dohPort)

	dataDir := rolodexTempDir(t, "rolodex-doh-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	writeRolodexDohConfig(t, dataDir, dnsPort, dohBackend)
	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (DoH ingress test)")

	ctx := testContext(t, 3*time.Minute)
	dohWaitForTLS(ctx, t, dohBackend, dataDir, key)

	// The hostname a client is told to use, built from the exported label so a
	// rename of DohHostLabel moves this test with it rather than past it.
	hostname := systemcontroller.DohHostLabel + ".doh-test.invalid"

	tlsDir := t.TempDir()
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := filepath.Join(tlsDir, "leaf")
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatalf("create leaf dir: %v", err)
	}
	if err := ca.IssueLeaf(leafDir, []string{hostname}); err != nil {
		t.Fatalf("IssueLeaf %s: %v", hostname, err)
	}

	httpsPort := dohFreePortInt(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	// The admin port is relocated for the same reason the two vhost ports are:
	// this container is --net host, so caddy's admin API lands in the host
	// namespace, and its default 2019 is a fixed port two concurrent test-full
	// runs would both claim — IRON RULE.
	srv := ingress.NewServer(sup, httpsPort, dohFreePortInt(t), "",
		ingress.WithCaddyAdminPort(dohFreePortInt(t)))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	t.Cleanup(func() { logCleanupf(t, sup.Shutdown(), "caddy shutdown") })

	// The route the product programs, in the shape dohIngressRoute builds it:
	// the whole vhost proxied to rolodex over https, verification skipped on the
	// internal hop only.
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname:   hostname,
		Backend:    dohBackend,
		CertDir:    leafDir,
		BackendTls: true,
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := dohClient(t, filepath.Join(tlsDir, "ca.crt"), httpsPort)
	query := dohEncodeQuery(0xbeef, "example.com.")
	url := "https://" + hostname + "/dns-query"

	var lastStatus int
	var lastBody []byte
	var lastErr error
	ok := dohPoll(func() bool {
		lastStatus, lastBody, lastErr = dohPost(ctx, client, url, query)
		return lastErr == nil && lastStatus == http.StatusOK
	})
	if !ok {
		dumpRolodexDiagnostics(ctx, t, dataDir, key)
		t.Fatalf("DoH query through the ingress never succeeded: status=%d err=%v body=%q",
			lastStatus, lastErr, truncateForLog(string(lastBody)))
	}

	// A well-formed DNS response, not a particular answer. Whether example.com
	// resolves depends on the network the suite runs on — a captive or offline
	// box SERVFAILs — and that is not what this test is for: the transport is.
	// The header is what proves rolodex answered rather than caddy returning an
	// error page with a 200.
	if err := dohCheckResponse(query, lastBody); err != nil {
		t.Errorf("%v\nbody: %x", err, lastBody)
	}
}

// writeRolodexDohConfig writes a bootstrap rolodex.yml carrying a `doh:`
// section, which is the only way that listener ever opens.
//
// It is spelled out here rather than reusing writeRolodexBootstrapConfig
// because the encrypted-transport sections are precisely what this test is
// about, and a helper that grew an optional DoH argument would let the section
// be silently dropped without the test noticing.
//
// auto_self_signed mirrors the install image: there is no provisioned
// certificate for a loopback hop, and a cert_path pointing at a file that does
// not exist is worse than a generated one. That certificate is exactly why the
// ingress has to skip verification on this hop.
func writeRolodexDohConfig(t *testing.T, dataDir, dnsPort, dohBind string) {
	t.Helper()

	config := fmt.Sprintf(`database_path: /data/rolodex.db
dns:
  bind:
    - udp: "%s:%s"
    - tcp: "%s:%s"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "8.8.8.8:53"
  - "8.8.4.4:53"
resolution:
  mode: auto
# off answers both A and AAAA and probes nothing. The default (auto)
# TCP-connects to hardcoded public addresses to decide which families the
# host can route, and filters answers of a family it could not reach --
# which makes what a test rolodex serves depend on the build machine's
# internet. See writeRolodexBootstrapConfig.
address_family:
  mode: "off"
doh:
  bind: "%s"
  tls:
    auto_self_signed: true
`, rolodex.DNSLoopback, dnsPort, rolodex.DNSLoopback, dnsPort, dohBind)

	if err := os.WriteFile(filepath.Join(dataDir, "rolodex.yml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write DoH rolodex.yml: %v", err)
	}
}

// dohWaitForTLS blocks until the DoH listener completes a TLS handshake.
//
// A TCP connect is not enough: rolodex binds the socket before its TLS stack is
// ready, so a test that proceeds on connect alone races the handshake and fails
// as a 502 from the ingress — which reads exactly like the misconfiguration
// this suite exists to catch.
func dohWaitForTLS(ctx context.Context, t *testing.T, addr, dataDir, key string) {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for the DoH listener: %v", ctx.Err())
		default:
		}
		dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		conn, err := (&tls.Dialer{
			// InsecureSkipVerify because the peer is self-signed by design:
			// this probe asks whether it speaks TLS at all, which is the
			// ingress's own question. Nothing is sent over the connection.
			Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		}).DialContext(dialCtx, "tcp", addr)
		cancel()
		if err == nil {
			logCleanupf(t, conn.Close(), "close DoH probe")
			return
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	dumpRolodexDiagnostics(ctx, t, dataDir, key)
	if raw, err := os.ReadFile(filepath.Join(dataDir, "rolodex.yml")); err == nil {
		t.Logf("rolodex.yml:\n%s", raw)
	}
	t.Fatalf("rolodex never opened a TLS DoH listener on %s: %v", addr, lastErr)
}

// dohEncodeQuery builds a minimal RFC 1035 query for name/A/IN.
//
// Hand-rolled rather than pulled from a DNS library: the wire format is the
// contract a DoH client and rolodex actually share, x/net/dns/dnsmessage is an
// indirect dependency this repo does not otherwise use, and thirty bytes of
// header and labels is less to get wrong than a new direct dependency.
func dohEncodeQuery(id uint16, name string) []byte {
	var b bytes.Buffer
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:], id)
	binary.BigEndian.PutUint16(hdr[2:], 0x0100) // RD
	binary.BigEndian.PutUint16(hdr[4:], 1)      // QDCOUNT
	b.Write(hdr)
	for label := range strings.SplitSeq(strings.TrimSuffix(name, "."), ".") {
		b.WriteByte(byte(len(label)))
		b.WriteString(label)
	}
	b.WriteByte(0)                                    // root label
	_ = binary.Write(&b, binary.BigEndian, uint16(1)) // QTYPE A
	_ = binary.Write(&b, binary.BigEndian, uint16(1)) // QCLASS IN
	return b.Bytes()
}

// dohCheckResponse verifies the bytes are a DNS response to this query, without
// caring what the answer was.
func dohCheckResponse(query, resp []byte) error {
	if len(resp) < 12 {
		return fmt.Errorf("response is %d bytes, too short to be a DNS message", len(resp))
	}
	if got, want := binary.BigEndian.Uint16(resp[0:]), binary.BigEndian.Uint16(query[0:]); got != want {
		return fmt.Errorf("response ID = %#04x, want %#04x — this is not an answer to our query", got, want)
	}
	if resp[2]&0x80 == 0 {
		return errors.New("QR bit is clear: the body is a query, not a response")
	}
	return nil
}

// dohPost performs one RFC 8484 POST.
func dohPost(ctx context.Context, client *http.Client, url string, query []byte) (int, []byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(query))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, body, err
}

// dohClient dials the ingress on its ephemeral port for every host, and trusts
// ONLY the test CA — no InsecureSkipVerify. Validating the ingress's leaf is
// the property that makes fronting a self-signed resolver worth doing.
func dohClient(t *testing.T, caPath string, port int) *http.Client {
	t.Helper()

	pem, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("CA certificate was not accepted into the pool")
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			},
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
}

// dohFindCaddy locates the caddy binary the ingress image ships, which the
// integration container carries at DefaultCaddyBinary.
// dohFreePortInt is findFreePort as the int the ingress server takes.
func dohFreePortInt(t *testing.T) int {
	t.Helper()
	n, err := strconv.Atoi(findFreePort(t))
	if err != nil {
		t.Fatalf("findFreePort returned %q: %v", findFreePort(t), err)
	}
	return n
}

// dohPoll retries cond for up to 30s. Caddy reloads asynchronously, so the
// first request after SetRoutes can land before the new vhost exists.
func dohPoll(cond func() bool) bool {
	for range 60 {
		if cond() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
