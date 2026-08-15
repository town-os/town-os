// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestDotServesDNSOverTLS is the first coverage the served DoT transport has had
// at any level.
//
// DoT is the one encrypted transport the ingress cannot front: the ingress
// publishes TCP only and its caddy is pinned to `protocols h1 h2`, so DoT
// terminates its own TLS and answers clients directly. On a real box the install
// image binds it on the LAN at :853; nothing in Town OS opens it, and nothing
// checked that the section the install image writes actually produces a
// listener that speaks RFC 7858. A typo in that section, a rolodex release that
// renames the key, or a TLS stack that never comes up all read the same way from
// a client — a connection that hangs — and all of them would ship green.
//
// The certificate is self-signed by design (auto_self_signed: there is no
// provisioned certificate for a listener that terminates its own TLS at first
// boot), so the client here skips verification. That is not the test looking the
// other way — it is what a DoT client on a real box has to do too, and it is
// written down in DESIGN.md next to the transport rather than discovered by
// whoever tries to point a phone at it.
func TestDotServesDNSOverTLS(t *testing.T) {
	t.Parallel()

	// Loopback and an ephemeral port, never the production 0.0.0.0:853: this
	// container runs --net host, so a well-known port here is the host's, and
	// two concurrent test-full runs would fight over it. IRON RULE.
	dnsPort := findFreePort(t)
	dotPort := findFreePort(t)
	dotAddr := net.JoinHostPort(rolodex.DNSLoopback, dotPort)

	dataDir := rolodexTempDir(t, "rolodex-dot-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	writeRolodexDotConfig(t, dataDir, dnsPort, dotAddr)
	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (DoT test)")

	ctx := testContext(t, 3*time.Minute)
	dohWaitForTLS(ctx, t, dotAddr, dataDir, key)

	query := dohEncodeQuery(0xd07, "example.com.")
	var resp []byte
	var lastErr error
	ok := dohPoll(func() bool {
		resp, lastErr = dotQuery(ctx, dotAddr, query)
		return lastErr == nil
	})
	if !ok {
		dumpRolodexDiagnostics(ctx, t, dataDir, key)
		t.Fatalf("DoT query never completed against %s: %v", dotAddr, lastErr)
	}

	// A well-formed response, not a particular answer: whether example.com
	// resolves depends on the network the suite runs on, and the transport is
	// what this test is for.
	if err := dohCheckResponse(query, resp); err != nil {
		t.Errorf("%v\nbody: %x", err, resp)
	}
}

// writeRolodexDotConfig writes a bootstrap rolodex.yml carrying a `dot:`
// section, which is the only way that listener ever opens — rolodex reads it
// once at startup and there is no gRPC call that can add one later.
//
// Spelled out rather than growing an optional argument on a shared helper, for
// the reason writeRolodexDohConfig gives: the encrypted-transport section is
// exactly what is under test, and an optional argument lets it be dropped
// silently.
func writeRolodexDotConfig(t *testing.T, dataDir, dnsPort, dotBind string) {
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
dot:
  bind: "%s"
  tls:
    auto_self_signed: true
`, rolodex.DNSLoopback, dnsPort, rolodex.DNSLoopback, dnsPort, dotBind)

	if err := os.WriteFile(filepath.Join(dataDir, "rolodex.yml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write DoT rolodex.yml: %v", err)
	}
}

// dotQuery performs one RFC 7858 exchange: a DNS message over TLS, each
// direction prefixed with its two-byte length, exactly as DNS over TCP.
//
// Hand-rolled for the reason dohEncodeQuery is: the wire format is the contract,
// and it is less to get wrong than a new direct dependency.
func dotQuery(ctx context.Context, addr string, query []byte) ([]byte, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := (&tls.Dialer{
		// The peer is self-signed by design — see the doc comment on the test.
		Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial DoT: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := dialCtx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set deadline: %w", err)
		}
	}

	framed := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(framed, uint16(len(query)))
	copy(framed[2:], query)
	if _, err := conn.Write(framed); err != nil {
		return nil, fmt.Errorf("write query: %w", err)
	}

	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("read length prefix: %w", err)
	}
	resp := make([]byte, binary.BigEndian.Uint16(lenBuf[:]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return resp, nil
}
