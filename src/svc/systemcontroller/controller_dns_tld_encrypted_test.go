// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// encryptedTLDServer is a controller serving `home`, with a real CA on a temp
// btrfs base so the DoT/DoQ leaf is genuinely issued and genuinely readable.
func encryptedTLDServer(t *testing.T) (*SystemdClient, *rolodex.MockClient, string) {
	t.Helper()

	btrfsBase := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	mock := &rolodex.MockClient{}
	ts := InitTestServer(ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		RolodexClient: mock,
		PagesMgr:      account.InitMockPagesManager(),
		SettingsMgr:   &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
		TLSCA:         ca,
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(ts.Close)
	ts.SetInternalIP("192.168.1.10")

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, mock, btrfsBase
}

// tldTestContext bounds the request so a wedged handler fails as this test
// rather than as the package's timeout.
func tldTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// recordsOfType returns every record of a type, as name→value pairs.
func recordsOfType(records []*upstream.DnsRecord, rtype upstream.RecordType) map[string][]string {
	out := map[string][]string{}
	for _, r := range records {
		if r.RecordType == rtype {
			out[r.Name] = append(out[r.Name], r.Value)
		}
	}
	return out
}

// TestTLDChangeRepublishesTheEncryptedDNSEndpoints is the regression test for a
// box that keeps advertising a resolver nobody can reach.
//
// `ChangeTLD` moves the zone and the package records. Encrypted DNS is named
// after the TLD in four other places — the DoH endpoint's address records, the
// DDR designation that advertises it, the SANs on the certificate DoT and DoQ
// serve, and the DANE pins for that certificate — and it moved none of them. A
// box renamed from `home` to `lan` went on telling every DDR client that its
// encrypted DNS was at `dns.home`, served `dns.lan` a certificate naming
// `dns.home`, and published no pin at the name a client would actually dial.
//
// The last of those is why this is a break rather than a downgrade: a
// DANE-aware client that reaches an endpoint and finds no TLSA record refuses
// the connection. Encrypted DNS stops working, and stays stopped until the next
// boot's RebuildDNS repairs it — which on this box can be months.
func TestTLDChangeRepublishesTheEncryptedDNSEndpoints(t *testing.T) {
	t.Parallel()

	c, mock, btrfsBase := encryptedTLDServer(t)
	ctx := tldTestContext(t)

	if err := c.SetupDNS(ctx); err != nil {
		t.Fatalf("SetupDNS: %v", err)
	}
	if err := c.SetDNSTLD(ctx, "lan"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}

	// 1. The name resolves under the new TLD.
	a := recordsOfType(mock.Records, upstream.RecordTypeA)
	if got := a["dns.lan."]; !slices.Contains(got, "192.168.1.10") {
		t.Errorf("A records for dns.lan. = %v, want the box's address — the DoH vhost has no name until this resolves", got)
	}

	// 2. The designation advertises the new name, and only the new name. A
	//    stale designation is worse than none: it sends every DDR client at a
	//    name that no longer resolves.
	svcb := recordsOfType(mock.Records, upstream.RecordTypeSVCB)
	designations := svcb[DDRDesignationName]
	if len(designations) == 0 {
		t.Fatalf("no DDR designation after the TLD change; SVCB records: %v", svcb)
	}
	for _, d := range designations {
		if !strings.Contains(d, "dns.lan.") {
			t.Errorf("designation %q does not name dns.lan.", d)
		}
		if strings.Contains(d, "dns.home.") {
			t.Errorf("designation %q still advertises the old TLD", d)
		}
	}

	// 3. The certificate DoT and DoQ serve names what a client now dials. A
	//    client that dials dns.lan and is handed a certificate for dns.home
	//    fails the hostname check, which is the same outage by another route.
	leaf := readLeaf(t, filepath.Join(btrfsBase, RolodexDataSubdir, RolodexTLSSubdir))
	if !slices.Contains(leaf.DNSNames, "dns.lan") {
		t.Errorf("leaf SANs = %v, want dns.lan", leaf.DNSNames)
	}

	// 4. And it is pinned where a client checks — under both transports,
	//    because DoT and DoQ are two endpoints sharing one certificate.
	tlsa := recordsOfType(mock.Records, upstream.RecordTypeTLSA)
	for _, owner := range []string{"_853._tcp.dns.lan.", "_853._udp.dns.lan."} {
		if len(tlsa[owner]) == 0 {
			t.Errorf("no DANE pin at %s; a client that checks DANE refuses an endpoint with no record", owner)
		}
	}
}

// TestTLDChangeLeavesNoPinAtTheOldName is the other half: a pin that outlives
// the name it was published for makes a DANE client refuse a certificate that
// is perfectly valid, which is the failure mode DANE is supposed to prevent.
func TestTLDChangeLeavesNoPinAtTheOldName(t *testing.T) {
	t.Parallel()

	c, mock, _ := encryptedTLDServer(t)
	ctx := tldTestContext(t)

	if err := c.SetupDNS(ctx); err != nil {
		t.Fatalf("SetupDNS: %v", err)
	}
	// Publish the old TLD's endpoints first, so there is something to retire —
	// SetupDNS establishes the zone, and a boot would have published these.
	if err := c.SetDNSTLD(ctx, "home"); err != nil {
		t.Fatalf("SetDNSTLD home: %v", err)
	}
	tlsa := recordsOfType(mock.Records, upstream.RecordTypeTLSA)
	if len(tlsa["_853._tcp.dns.home."]) == 0 {
		t.Fatal("precondition failed: no pin was published for the original TLD")
	}

	if err := c.SetDNSTLD(ctx, "lan"); err != nil {
		t.Fatalf("SetDNSTLD lan: %v", err)
	}

	tlsa = recordsOfType(mock.Records, upstream.RecordTypeTLSA)
	for _, owner := range []string{"_853._tcp.dns.home.", "_853._udp.dns.home."} {
		if len(tlsa[owner]) > 0 {
			t.Errorf("pin at %s survived the TLD change: %v", owner, tlsa[owner])
		}
	}
	// The DoH endpoint's old address record must go too, or the old name keeps
	// resolving to a vhost the ingress no longer serves.
	if got := recordsOfType(mock.Records, upstream.RecordTypeA)["dns.home."]; len(got) > 0 {
		t.Errorf("dns.home. still resolves after the rename: %v", got)
	}
}
