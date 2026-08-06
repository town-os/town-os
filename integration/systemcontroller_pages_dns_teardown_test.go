// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// pageTeardownHarness is the fixture for the page DNS-teardown integration
// tests: a REAL rolodex container (with a live resolver on dnsPort), a real
// local CA so page leaves are issued and their DANE pins are computable, and a
// real network manager so a page can be put on a non-default network.
type pageTeardownHarness struct {
	client    *systemcontroller.SystemdClient
	rolo      rolodex.Client
	dnsPort   string
	pages     account.PagesManager
	networks  account.NetworkManager
	settings  account.SettingsManager
	btrfsBase string
}

const (
	pageTeardownLANIP  = "192.168.122.60"
	pageTeardownLANIP6 = "2001:db8:beef::60"
)

// pageTeardownCtx is the deadline-aware context these tests run under, leaving
// headroom for the rolodex container's cleanup.
func pageTeardownCtx(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}
	return ctx
}

func initPageTeardownTest(t *testing.T) *pageTeardownHarness {
	t.Helper()

	realClient, dnsPort := initRolodexRealTest(t)

	btrfsBase := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if err := systemcontroller.EnsurePagesWebroot(btrfsBase); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	nm := initNetworkDB(t)
	pagesMgr := account.InitMockPagesManager()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		Systemd:       systemd.InitMockManager(),
		RolodexClient: realClient,
		IngressClient: &ingress.MockClient{},
		SettingsMgr:   settings,
		NetworkMgr:    nm,
		PagesMgr:      pagesMgr,
		TLSCA:         ca,
		BtrfsBasePath: btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	// A dual-stack box. This is what exposes the bug: the page create path
	// publishes only an A record, while the boot rebuild publishes the AAAA
	// alongside it, so the two halves of the name have different owners.
	ts.SetInternalIP(pageTeardownLANIP)
	ts.SetInternalIPv6(pageTeardownLANIP6)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Boot-time network reconcile, exactly as main.go runs it — this creates the
	// "home" network row and its rolodex scope, without which a page targeting
	// the default network is rejected.
	systemcontroller.ReconcileNetworks(pageTeardownCtx(t), systemcontroller.ReconcileNetworksConfig{
		NetworkMgr:       nm,
		Systemd:          systemd.InitMockManager(),
		NetworkStatePath: t.TempDir(),
		SettingsMgr:      settings,
		RolodexClient:    realClient,
	})

	return &pageTeardownHarness{
		client:    c,
		rolo:      realClient,
		dnsPort:   dnsPort,
		pages:     pagesMgr,
		networks:  nm,
		settings:  settings,
		btrfsBase: btrfsBase,
	}
}

// bootRebuild replays what the systemcontroller does on every start: tear the
// zone down and republish it. This is where a page's AAAA record and its DANE
// TLSA pin come from — neither is written by the create path — so it is the
// step that turns "page removed" into "page name still in DNS".
func (h *pageTeardownHarness) bootRebuild(t *testing.T, ctx context.Context) {
	t.Helper()
	cfg := systemcontroller.ReconcileDNSConfig{
		Client:        h.rolo,
		SettingsMgr:   h.settings,
		PagesManager:  h.pages,
		NetworkMgr:    h.networks,
		InternalIP:    pageTeardownLANIP,
		InternalIPv6:  pageTeardownLANIP6,
		BtrfsBasePath: h.btrfsBase,
	}
	if err := systemcontroller.RebuildDNS(ctx, cfg); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}
	if err := systemcontroller.RebuildNetworkDNS(ctx, cfg); err != nil {
		t.Fatalf("RebuildNetworkDNS: %v", err)
	}
}

// globalRecordsUnder returns every global record whose owner name is, or is
// suffixed by, host — so a TLSA at _443._tcp.<host> counts as "<host> is still
// in DNS", which is how an operator reads the DNS records screen.
func globalRecordsUnder(t *testing.T, ctx context.Context, rc rolodex.Client, host string) []*upstream.DnsRecord {
	t.Helper()
	all, err := rc.ListRecords(ctx, nil)
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

// scopedRecordsUnder is globalRecordsUnder for a network scope's own table.
func scopedRecordsUnder(t *testing.T, ctx context.Context, rc rolodex.Client, scope, host string) []*upstream.DnsRecord {
	t.Helper()
	all, err := rc.ListScopedRecords(ctx, scope, nil)
	if err != nil {
		t.Fatalf("ListScopedRecords: %v", err)
	}
	var out []*upstream.DnsRecord
	for _, r := range all {
		if r.Name == host || strings.HasSuffix(r.Name, "."+host) {
			out = append(out, r)
		}
	}
	return out
}

func summarizeRecords(recs []*upstream.DnsRecord) string {
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

// resolvesEventuallyGone polls the live resolver until the name stops resolving
// or ctx expires, returning whatever it last saw. A name that never goes away
// is the user-visible form of the bug: the page is gone from the UI and from
// disk, and the hostname still answers.
func resolvesEventuallyGone(ctx context.Context, t *testing.T, dnsPort, name string) []string {
	t.Helper()
	r := lanResolver(dnsPort)
	var addrs []string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		got, err := r.LookupHost(lookupCtx, name)
		cancel()
		if err != nil || len(got) == 0 {
			return nil
		}
		addrs = got
		time.Sleep(200 * time.Millisecond)
	}
	return addrs
}

// TestIntegrationRemovePageRetiresItsNameFromRealDNS is the end-to-end
// regression test for the reported bug: the page "erik" was deleted and
// erik.home stayed resolvable.
//
// It drives the real HTTP create/remove path against a REAL rolodex, with a
// boot rebuild in between (which is what publishes the AAAA record and the DANE
// TLSA pin the create path never writes), and then asks the live resolver
// whether the name is gone. Removal used to drop only the A record, so on any
// box that had restarted since the page was created the name kept answering
// over IPv6 and kept a TLSA pin pointing at a certificate for a page that no
// longer existed.
func TestIntegrationRemovePageRetiresItsNameFromRealDNS(t *testing.T) {
	t.Parallel()

	ctx := pageTeardownCtx(t)
	h := initPageTeardownTest(t)

	if _, err := h.client.CreatePage(ctx, "erik", "", "", "", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage erik: %v", err)
	}

	const host = "erik.home."
	const tlsaOwner = "_443._tcp.erik.home."

	// Precondition: the page resolves before the restart.
	if addrs := resolveEventually(ctx, t, lanResolver(h.dnsPort), host); len(addrs) == 0 {
		t.Fatalf("precondition failed: %s did not resolve after create", host)
	}

	// The restart that turns the leak on.
	h.bootRebuild(t, ctx)

	recs := globalRecordsUnder(t, ctx, h.rolo, host)
	var sawAAAA, sawTLSA bool
	for _, r := range recs {
		switch r.RecordType {
		case upstream.RecordTypeAAAA:
			sawAAAA = true
		case upstream.RecordTypeTLSA:
			sawTLSA = true
		}
	}
	if !sawAAAA {
		t.Fatalf("precondition failed: expected an AAAA for %s after the boot rebuild; got %s",
			host, summarizeRecords(recs))
	}
	if !sawTLSA {
		t.Fatalf("precondition failed: expected a TLSA pin at %s after the boot rebuild; got %s",
			tlsaOwner, summarizeRecords(recs))
	}

	if err := h.client.RemovePage(ctx, "erik"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	if left := globalRecordsUnder(t, ctx, h.rolo, host); len(left) > 0 {
		t.Errorf("erik.home is still in DNS after the page was removed: %s", summarizeRecords(left))
	}
	if addrs := resolvesEventuallyGone(ctx, t, h.dnsPort, host); len(addrs) > 0 {
		t.Errorf("a removed page still resolves: %s -> %v", host, addrs)
	}
}

// TestIntegrationRemoveNetworkPageRetiresBothHomes is the dual-homed case: a
// page on a WireGuard network carries a scoped record and pin (for overlay
// peers) as well as a global record and pin (for LAN clients). Removing it must
// retire both halves — leaving either one behind keeps the name alive for one
// class of client while the operator sees it gone from the UI.
func TestIntegrationRemoveNetworkPageRetiresBothHomes(t *testing.T) {
	t.Parallel()

	ctx := pageTeardownCtx(t)
	h := initPageTeardownTest(t)

	if _, err := h.client.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if _, err := h.client.CreatePage(ctx, "secret", "", "", "secret", account.PageSourceArchive, "", "", "fart"); err != nil {
		t.Fatalf("CreatePage secret: %v", err)
	}

	const host = "secret.fart."
	h.bootRebuild(t, ctx)

	if got := globalRecordsUnder(t, ctx, h.rolo, host); len(got) == 0 {
		t.Fatalf("precondition failed: expected global (LAN) records for %s after the boot rebuild", host)
	}
	if got := scopedRecordsUnder(t, ctx, h.rolo, "fart", host); len(got) == 0 {
		t.Fatalf("precondition failed: expected scoped (overlay) records for %s after the boot rebuild", host)
	}

	if err := h.client.RemovePage(ctx, "secret"); err != nil {
		t.Fatalf("RemovePage: %v", err)
	}

	if left := globalRecordsUnder(t, ctx, h.rolo, host); len(left) > 0 {
		t.Errorf("the LAN half of %s survived the page removal: %s", host, summarizeRecords(left))
	}
	if left := scopedRecordsUnder(t, ctx, h.rolo, "fart", host); len(left) > 0 {
		t.Errorf("the overlay half of %s survived the page removal: %s", host, summarizeRecords(left))
	}
	if addrs := resolvesEventuallyGone(ctx, t, h.dnsPort, host); len(addrs) > 0 {
		t.Errorf("a removed network page still resolves on the LAN: %s -> %v", host, addrs)
	}
}
