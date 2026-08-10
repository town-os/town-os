// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// pagesDNSEnv is the fixture for the page DNS-teardown tests: the HTTP client,
// the in-memory rolodex the handlers program, and the pieces a test needs to
// replay what boot does (RebuildDNS) before removing a page.
type pagesDNSEnv struct {
	client    *SystemdClient
	rolo      *rolodex.MockClient
	pages     account.PagesManager
	settings  account.SettingsManager
	networks  account.NetworkManager
	btrfsBase string
	ts        *TestServer
}

// initPagesDNSEnv wires a systemcontroller with a mock rolodex, a mock ingress
// (so page leaves are actually issued, which is what makes a page's DANE TLSA
// computable), a real local CA, and a pinned LAN IPv4 + global IPv6 — the shape
// of a real box, where a page ends up with an A record, an AAAA record, and a
// TLSA pin all naming the same host.
//
// withNetworks adds a real network manager (which seeds the home network), so a
// test can put a page on a non-default network and exercise the dual-homed
// teardown. Without it the network manager is nil, which is the plain
// single-network box.
func initPagesDNSEnv(t *testing.T, withNetworks bool) pagesDNSEnv {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	var networks account.NetworkManager
	if withNetworks {
		nm, err := account.InitNetworkManager(t.Context(), db)
		if err != nil {
			t.Fatalf("InitNetworkManager: %v", err)
		}
		networks = nm
	}
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	btrfsBase := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if err := EnsurePagesWebroot(btrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	rolo := &rolodex.MockClient{}
	pagesMgr := account.InitMockPagesManager()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	ts := InitTestServer(ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		AccountMgr:    mgr,
		SessionMgr:    sessMgr,
		PagesMgr:      pagesMgr,
		NetworkMgr:    networks,
		Git:           git.InitMockClient(),
		RolodexClient: rolo,
		IngressClient: &ingress.MockClient{},
		SettingsMgr:   settings,
		TLSCA:         ca,
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(ts.Close)

	// A real box has both, and that is precisely what exposes the bug: the
	// create path publishes only the A record, while the boot/hourly reconcile
	// publishes the AAAA alongside it.
	ts.SetInternalIP("192.168.1.10")
	ts.SetInternalIPv6("2001:db8::10")

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	if _, err := c.CreateAccount(context.TODO(), "pagesadmin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "pagesadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return pagesDNSEnv{
		client:    c,
		rolo:      rolo,
		pages:     pagesMgr,
		settings:  settings,
		networks:  networks,
		btrfsBase: btrfsBase,
		ts:        ts,
	}
}

// rebuildDNS replays the boot-time zone rebuild, which is what publishes a
// page's AAAA record and its DANE TLSA pin — neither of which the create path
// writes. Any page record that outlives a removal was almost certainly put
// there by this pass on a previous boot.
func (e pagesDNSEnv) rebuildDNS(t *testing.T) {
	t.Helper()
	if err := RebuildDNS(context.Background(), ReconcileDNSConfig{
		Client:        e.rolo,
		SettingsMgr:   e.settings,
		PagesManager:  e.pages,
		NetworkMgr:    e.networks,
		InternalIP:    "192.168.1.10",
		InternalIPv6:  "2001:db8::10",
		BtrfsBasePath: e.btrfsBase,
	}); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}
}

// recordsFor returns every global record whose owner name is exactly fqdn.
func recordsFor(t *testing.T, rc *rolodex.MockClient, fqdn string) []*upstream.DnsRecord {
	t.Helper()
	all, err := rc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	var out []*upstream.DnsRecord
	for _, r := range all {
		if r.Name == fqdn {
			out = append(out, r)
		}
	}
	return out
}

// recordsMentioning returns every global record whose owner name is, or is
// suffixed by, host — so a TLSA at _443._tcp.<host> counts as "<host> is still
// in DNS", which is exactly how an operator reads the records screen.
func recordsMentioning(t *testing.T, rc *rolodex.MockClient, host string) []*upstream.DnsRecord {
	t.Helper()
	all, err := rc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	var out []*upstream.DnsRecord
	for _, r := range all {
		if r.Name == host || strings.HasSuffix(r.Name, "."+host) {
			out = append(out, r)
		}
	}
	return out
}

func describeRecords(recs []*upstream.DnsRecord) string {
	var b strings.Builder
	for i, r := range recs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(r.Name)
		b.WriteString(" ")
		b.WriteString(r.RecordType.String())
		b.WriteString(" ")
		b.WriteString(r.Value)
	}
	if b.Len() == 0 {
		return "<none>"
	}
	return b.String()
}

// TestRemovePageClearsEveryRecordForItsName is the regression test for the
// reported bug: a page named "erik" was deleted and erik.home stayed in DNS.
//
// The create path publishes exactly one thing — a global A record — but the
// boot rebuild publishes an AAAA alongside it and a DANE TLSA pin at
// _443._tcp.erik.home. Removal only ever asked rolodex to drop the A, so on
// any box that had been restarted since the page was created (i.e. any box
// that has actually been running), the name survived its own deletion.
func TestRemovePageClearsEveryRecordForItsName(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Boot rebuild: this is where the AAAA and the TLSA come from.
	env.rebuildDNS(t)

	const host = "erik.home."
	if got := recordsMentioning(t, env.rolo, host); len(got) == 0 {
		t.Fatalf("precondition failed: expected erik.home records after rebuild, got none")
	}

	if err := env.client.RemovePage(ctx, "erik"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	if got := recordsMentioning(t, env.rolo, host); len(got) > 0 {
		t.Fatalf("erik.home is still in DNS after the page was removed: %s", describeRecords(got))
	}
}

// TestRemovePageClearsAAAARecord isolates the address half of the bug: the A
// record is dropped and the AAAA is not, so the name still resolves over IPv6
// on any dual-stack box.
func TestRemovePageClearsAAAARecord(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	env.rebuildDNS(t)

	const host = "erik.home."
	if len(recordsFor(t, env.rolo, host)) < 2 {
		t.Fatalf("precondition failed: expected an A and an AAAA for %s, got %s",
			host, describeRecords(recordsFor(t, env.rolo, host)))
	}

	if err := env.client.RemovePage(ctx, "erik"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	for _, r := range recordsFor(t, env.rolo, host) {
		if r.RecordType == upstream.RecordTypeAAAA {
			t.Fatalf("AAAA record for %s survived the page removal (value %s); "+
				"the name still resolves over IPv6", host, r.Value)
		}
		if r.RecordType == upstream.RecordTypeA {
			t.Fatalf("A record for %s survived the page removal (value %s)", host, r.Value)
		}
	}
}

// TestRemovePageClearsTLSARecord isolates the DANE half. The TLSA pin is the
// leak that never self-heals: ReconcileDNS (the hourly drift repair) only
// indexes A and AAAA records, so an orphan TLSA is never diffed away and
// survives until a controller restart tears the whole zone down.
func TestRemovePageClearsTLSARecord(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	env.rebuildDNS(t)

	const tlsaOwner = "_443._tcp.erik.home."
	if len(recordsFor(t, env.rolo, tlsaOwner)) == 0 {
		t.Fatalf("precondition failed: expected a TLSA pin at %s after the rebuild", tlsaOwner)
	}

	if err := env.client.RemovePage(ctx, "erik"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	if got := recordsFor(t, env.rolo, tlsaOwner); len(got) > 0 {
		t.Fatalf("TLSA pin for a removed page survived: %s", describeRecords(got))
	}
}

// TestRemovePageOrphanRecordsSurviveHourlyReconcile proves the TLSA leak is
// permanent rather than merely slow: running the drift-repair pass that is
// supposed to converge the zone leaves the orphan pin exactly where it was,
// because ReconcileDNS only reconciles address records.
func TestRemovePageOrphanRecordsSurviveHourlyReconcile(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	env.rebuildDNS(t)
	if err := env.client.RemovePage(ctx, "erik"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	// The hourly drift repair, exactly as the poller runs it.
	if err := ReconcileDNS(ctx, ReconcileDNSConfig{
		Client:        env.rolo,
		SettingsMgr:   env.settings,
		PagesManager:  env.pages,
		InternalIP:    "192.168.1.10",
		InternalIPv6:  "2001:db8::10",
		BtrfsBasePath: env.btrfsBase,
	}); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	if got := recordsMentioning(t, env.rolo, "erik.home."); len(got) > 0 {
		t.Fatalf("erik.home records outlived both the removal and the drift repair: %s",
			describeRecords(got))
	}
}

// TestPinnedInternalIPSurvivesIPv6Read pins the test seam that made the page
// AAAA change break an unrelated integration test.
//
// GetInternalIPv6 lazily refreshes an empty cache, and RefreshInternalIP
// rediscovers BOTH families. So on a server whose IPv4 alone had been pinned,
// the first read of the IPv6 address silently replaced that pin with the host's
// real LAN address — and every DNS record written after that read named the
// machine running the tests instead of the address the test chose. It surfaced
// as a page on a WireGuard network resolving to the developer's own wifi IP.
//
// Eight integration test files pin the IPv4 alone, so this is a trap for any
// future code path that reads the IPv6, not just for pages.
func TestPinnedInternalIPSurvivesIPv6Read(t *testing.T) {
	t.Parallel()
	// The seam under test is the address cache on the embedded serverBase; no
	// router or listener is involved, so construct the TestServer directly
	// rather than standing up an HTTP server that would need managers wired.
	ts := &TestServer{}

	const pinned = "192.168.122.50"
	ts.SetInternalIP(pinned)

	// The read that used to clobber it.
	if got := ts.GetInternalIPv6(); got != "" {
		t.Fatalf("an unpinned IPv6 must read as empty, not be discovered from the host; got %q", got)
	}
	if got := ts.GetInternalIP(); got != pinned {
		t.Fatalf("reading the IPv6 discarded the pinned IPv4: got %q, want %q", got, pinned)
	}
}

// TestPinnedInternalIPv6SurvivesIPv4Pin pins the order-independence: claiming a
// slot must never erase a value the test already put there.
func TestPinnedInternalIPv6SurvivesIPv4Pin(t *testing.T) {
	t.Parallel()
	ts := &TestServer{}

	const v6 = "2001:db8::10"
	const v4 = "192.168.1.10"
	ts.SetInternalIPv6(v6)
	ts.SetInternalIP(v4)

	if got := ts.GetInternalIPv6(); got != v6 {
		t.Errorf("pinning the IPv4 erased the IPv6: got %q, want %q", got, v6)
	}
	if got := ts.GetInternalIP(); got != v4 {
		t.Errorf("IPv4 = %q, want %q", got, v4)
	}
}

// TestCreatePagePublishesBothAddressFamilies pins the other half of the fix:
// create and remove must touch the same record set. The create path used to
// publish only an A record, leaving the AAAA to whichever reconcile ran next —
// which is exactly how the two ends of a page's life came to disagree about
// what a page's name consists of.
func TestCreatePagePublishesBothAddressFamilies(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	var sawA, sawAAAA bool
	for _, r := range recordsFor(t, env.rolo, "erik.home.") {
		switch r.RecordType {
		case upstream.RecordTypeA:
			sawA = true
			if r.Value != "192.168.1.10" {
				t.Errorf("A value = %q, want the LAN IP", r.Value)
			}
		case upstream.RecordTypeAAAA:
			sawAAAA = true
			if r.Value != "2001:db8::10" {
				t.Errorf("AAAA value = %q, want the host's global IPv6", r.Value)
			}
		}
	}
	if !sawA {
		t.Error("create published no A record")
	}
	if !sawAAAA {
		t.Error("create published no AAAA record, so removal has nothing to pair with")
	}
}

// TestCreatePagePublishesNoAAAAOnV4OnlyHost is the back-compat half: a host
// with no global IPv6 must publish the A record and nothing else, so the
// symmetry fix cannot invent a record pointing at an address the box does not
// have.
func TestCreatePagePublishesNoAAAAOnV4OnlyHost(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	env.ts.SetInternalIPv6("")
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	for _, r := range recordsFor(t, env.rolo, "erik.home.") {
		if r.RecordType == upstream.RecordTypeAAAA {
			t.Fatalf("v4-only host published an AAAA record: %s", describeRecords(recordsFor(t, env.rolo, "erik.home.")))
		}
	}
}

// TestRemoveNetworkPageClearsScopedAndGlobalRecords covers the dual-homed page.
// A page on a WireGuard network carries a scoped record and a scoped DANE pin
// (for overlay peers) as well as a global record and pin (for LAN clients).
// The scoped pin is the subtle one: it lives at _443._tcp.<host>, a different
// owner name from the address record, so clearing the host's scoped records
// does not reach it.
func TestRemoveNetworkPageClearsScopedAndGlobalRecords(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, true)
	ctx := context.Background()

	if _, err := env.client.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if _, err := env.client.CreatePage(ctx, "secret", "", "", "secret", account.PageSourceArchive, "", "", "fart"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// The boot rebuild publishes both halves of the dual home, pins included.
	if err := RebuildNetworkDNS(ctx, ReconcileDNSConfig{
		Client:        env.rolo,
		SettingsMgr:   env.settings,
		PagesManager:  env.pages,
		NetworkMgr:    env.networks,
		InternalIP:    "192.168.1.10",
		InternalIPv6:  "2001:db8::10",
		BtrfsBasePath: env.btrfsBase,
	}); err != nil {
		t.Fatalf("RebuildNetworkDNS: %v", err)
	}

	const host = "secret.fart."
	if got := recordsMentioning(t, env.rolo, host); len(got) == 0 {
		t.Fatalf("precondition failed: expected global records for %s after the rebuild", host)
	}

	if err := env.client.RemovePage(ctx, "secret"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	if got := recordsMentioning(t, env.rolo, host); len(got) > 0 {
		t.Errorf("the LAN half of %s survived the removal: %s", host, describeRecords(got))
	}

	scoped, err := env.rolo.ListScopedRecords(ctx, "fart", nil)
	if err != nil {
		t.Fatalf("ListScopedRecords: %v", err)
	}
	for _, r := range scoped {
		if r.Name == host || strings.HasSuffix(r.Name, "."+host) {
			t.Errorf("the overlay half of %s survived the removal: %s %s %s",
				host, r.Name, r.RecordType.String(), r.Value)
		}
	}
}

// TestUpdatePageDomainClearsEveryOldRecord covers the same teardown through the
// other caller: renaming a page's domain must retire the old name completely,
// not just its A record, or the old hostname keeps resolving forever.
func TestUpdatePageDomainClearsEveryOldRecord(t *testing.T) {
	t.Parallel()
	env := initPagesDNSEnv(t, false)
	ctx := context.Background()

	if _, err := env.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	env.rebuildDNS(t)

	renamed := "erik2"
	if _, err := env.client.UpdatePage(ctx, "erik", account.PageSiteUpdate{Domain: &renamed}); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	if got := recordsMentioning(t, env.rolo, "erik.home."); len(got) > 0 {
		t.Fatalf("the old page hostname is still in DNS after a domain change: %s",
			describeRecords(got))
	}
}
