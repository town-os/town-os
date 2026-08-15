// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
	upstream "gitea.com/town-os/rolodex-dns/go"
)

// TestDotAdoptsTheCAIssuedLeafWithoutARestart is the end-to-end proof for the
// handoff the whole DoT/DoQ certificate arrangement rests on.
//
// The design is: ../install points `dot.tls` at /data/tls/dot/{cert,key}.pem
// AND leaves auto_self_signed on, the systemcontroller writes that pair when
// the CA exists, and rolodex — which started first, before there was a CA —
// swaps to it on its own within one 30-second poll. Nothing calls rolodex. The
// file appearing IS the handoff.
//
// Every part of that is invisible to a unit test. issueRolodexTransportLeaf is
// unit-tested for writing the right file in the right directory, but the claim
// that matters is about a *different process* noticing: that rolodex accepts a
// named-but-absent certificate at startup rather than refusing to boot, that it
// polls the path by name, and that it starts serving the new pair with no
// restart and no dropped listener. A regression in any of those looks identical
// from outside — a box that quietly serves a self-signed certificate forever —
// and would ship green.
//
// It also establishes the half of the DANE claim that integration alone can
// reach. collectRolodexTransportTLSA derives the pin from the file on disk; this
// proves the certificate rolodex actually PRESENTS is that same file. Those two
// together are what make the published pin match the served certificate — and
// if they ever disagree, DANE fails closed and the feature is worse than absent.
func TestDotAdoptsTheCAIssuedLeafWithoutARestart(t *testing.T) {
	t.Parallel()

	// Loopback and ephemeral ports, never the production 0.0.0.0:853: this
	// container runs --net host, so a well-known port here is the host's and
	// two concurrent test-full runs would fight over it. IRON RULE.
	dnsPort := findFreePort(t)
	dotPort := findFreePort(t)
	dotAddr := net.JoinHostPort(rolodex.DNSLoopback, dotPort)

	dataDir := rolodexTempDir(t, "rolodex-dot-dane-*")
	leafDir := filepath.Join(dataDir, systemcontroller.RolodexTLSSubdir)

	sd := systemd.NewManager()
	key := rolodexTestKey()
	// The container sees the data directory as /data, which is why the config
	// names /data/tls/dot rather than the host path — the same two-halves-of-
	// one-mount arrangement ../install writes on a real box.
	writeRolodexDotDaneConfig(t, dataDir, dnsPort, dotAddr)
	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (DoT DANE test)")

	ctx := testContext(t, 5*time.Minute)
	dohWaitForTLS(ctx, t, dotAddr, dataDir, key)

	// The control, and the reason this test can tell adoption from coincidence:
	// with no leaf on disk yet, rolodex must already be serving — on generated
	// material. If it refused to start, or if a leaf were somehow present here,
	// the assertion after the swap would prove nothing.
	before, err := dotServedCertificate(ctx, dotAddr)
	if err != nil {
		dumpRolodexDiagnostics(ctx, t, dataDir, key)
		t.Fatalf("DoT listener never presented a certificate: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(leafDir, townostls.LeafCertFileName)); statErr == nil {
		t.Fatal("a leaf already existed before the CA issued one; the swap below would prove nothing")
	}

	// Now do what the systemcontroller does on its first reconcile once the CA
	// exists: issue the leaf into the directory rolodex is watching.
	ca, err := townostls.EnsureCA(filepath.Join(dataDir, "ca"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if err := ca.IssueLeaf(leafDir, []string{"dns.fart", "127.0.0.1"}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}
	issued := readLeafCert(t, filepath.Join(leafDir, townostls.LeafCertFileName))

	// rolodex polls every 30s, so allow comfortably more than one interval.
	var after *x509.Certificate
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		cur, curErr := dotServedCertificate(ctx, dotAddr)
		if curErr == nil && cur.Equal(issued) {
			after = cur
			break
		}
		time.Sleep(2 * time.Second)
	}
	if after == nil {
		dumpRolodexDiagnostics(ctx, t, dataDir, key)
		t.Fatalf("rolodex never adopted the CA-issued leaf at %s within 90s", leafDir)
	}

	// Adoption, stated as the change it is: the served certificate is no longer
	// the generated one.
	if before.Equal(issued) {
		t.Error("the generated certificate already equalled the issued leaf; nothing was proven")
	}

	// And the certificate presented is signed by the box's CA, which is the
	// point of issuing it — a client that trusts the CA can verify it without
	// pinning anything.
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, verifyErr := after.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); verifyErr != nil {
		t.Errorf("served leaf does not chain to the box CA: %v", verifyErr)
	}
}

// TestRolodexPublishesTheDoQPinUnderUDP proves the owner name a DoQ client
// looks up is the one that actually lands in a live rolodex.
//
// rolodex.TLSAEntry carried no protocol until this feature: tlsaName hardcoded
// _tcp, so the DoQ half of an encrypted-DNS pin could not be expressed. The unit
// test covers the string construction against a mock client; this covers the
// part a mock cannot — that a real rolodex accepts a record at
// _853._udp.<name>, stores it as TLSA, and gives it back under that exact
// owner. A pin published under the wrong owner name is not a wrong record, it
// is an ABSENT one, and absence is what makes a DANE client fail closed.
func TestRolodexPublishesTheDoQPinUnderUDP(t *testing.T) {
	t.Parallel()

	dnsPort := findFreePort(t)
	dataDir := rolodexTempDir(t, "rolodex-dane-udp-*")
	socketPath := filepath.Join(dataDir, "rolodex.sock")

	sd := systemd.NewManager()
	key := rolodexTestKey()
	writeRolodexBootstrapConfig(t, dataDir, dnsPort, findFreePort(t))
	startRolodexUnit(t, sd, key, dataDir, "Rolodex DNS (DANE udp test)")

	ctx := testContext(t, 3*time.Minute)
	client := waitForRolodexClient(t, ctx, socketPath, dataDir, key)
	defer func() { logCleanupf(t, client.Close(), "close rolodex client") }()

	// The pin value is opaque here — what is under test is the owner name, so a
	// syntactically valid DANE-EE RDATA is enough.
	const pin = "3 1 1 " + "0000000000000000000000000000000000000000000000000000000000000000"
	entries := []rolodex.TLSAEntry{
		{Name: "dns.fart", Port: 853, Proto: "tcp", Value: pin},
		{Name: "dns.fart", Port: 853, Proto: "udp", Value: pin},
	}
	if err := rolodex.RegisterPackageTLSA(ctx, client, entries); err != nil {
		dumpRolodexDiagnostics(ctx, t, dataDir, key)
		t.Fatalf("RegisterPackageTLSA: %v", err)
	}

	records, err := client.ListRecords(ctx, nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	found := map[string]string{}
	for _, r := range records {
		if r.RecordType == upstream.RecordTypeTLSA {
			found[r.Name] = r.Value
		}
	}

	for _, owner := range []string{"_853._tcp.dns.fart.", "_853._udp.dns.fart."} {
		if got, ok := found[owner]; !ok {
			// The owners that ARE stored, so a mismatch names what was there
			// rather than only what was missing.
			t.Errorf("no TLSA record at %s; stored owners: %v",
				owner, slices.Sorted(maps.Keys(found)))
		} else if got != pin {
			t.Errorf("TLSA at %s = %q, want %q", owner, got, pin)
		}
	}

	// The control. Before Proto existed every entry landed under _tcp, so a
	// regression to that would still satisfy the tcp assertion above and leave
	// the udp one failing — but it would ALSO leave this owner empty, which is
	// the shape of the bug rather than of a typo.
	if _, ok := found["_853._udp.dns.fart"]; ok {
		t.Error("a record landed at the unqualified owner; names must carry the trailing dot")
	}
}

// writeRolodexDotDaneConfig writes the config ../install writes for encrypted
// DNS on a real box: cert_path/key_path naming a file that does not exist yet,
// AND auto_self_signed left on.
//
// Both, deliberately — that combination is the entire mechanism. rolodex treats
// a named-but-absent certificate as "serve a generated one and watch for the
// real one", which is what lets the resolver start before the CA that will
// eventually issue its certificate exists. Dropping either half turns the box
// into one that either refuses to boot or never stops being self-signed.
func writeRolodexDotDaneConfig(t *testing.T, dataDir, dnsPort, dotBind string) {
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
    cert_path: "/data/%s/%s"
    key_path: "/data/%s/%s"
    auto_self_signed: true
`,
		rolodex.DNSLoopback, dnsPort, rolodex.DNSLoopback, dnsPort, dotBind,
		systemcontroller.RolodexTLSSubdir, townostls.LeafCertFileName,
		systemcontroller.RolodexTLSSubdir, townostls.LeafKeyFileName,
	)

	if err := os.WriteFile(filepath.Join(dataDir, "rolodex.yml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write DoT DANE rolodex.yml: %v", err)
	}
}

// dotServedCertificate returns the leaf a DoT client is actually presented.
//
// Verification is skipped because the identity is what is being INSPECTED here
// rather than trusted: before the swap the peer is self-signed, and after it the
// certificate's provenance is checked explicitly against the CA.
func dotServedCertificate(ctx context.Context, addr string) (*x509.Certificate, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := (&tls.Dialer{
		Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial DoT: %w", err)
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, fmt.Errorf("dialer returned %T, not a *tls.Conn", conn)
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("peer presented no certificate")
	}
	return certs[0], nil
}

// readLeafCert parses a PEM leaf from disk.
func readLeafCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read leaf %s: %v", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("leaf %s is not PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf %s: %v", path, err)
	}
	return cert
}
