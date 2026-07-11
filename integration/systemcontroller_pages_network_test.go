// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"slices"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestIntegrationPageOnNetworkDualHomesAndResolvesOnLAN drives the real HTTP
// create path against REAL rolodex and a live resolver, and asserts a page
// assigned to a non-default WireGuard network gets the same treatment a package
// does:
//
//   - it is named under the NETWORK's TLD (blog.fart), never the global dns_tld;
//   - it is DUAL-HOMED — a scoped record at the overlay IP (for WireGuard peers)
//     and a global record at the LAN IP (for LAN clients, which cannot route to
//     the overlay);
//   - it does NOT leak into the global home zone;
//   - the ingress serves it at that same FQDN.
//
// TLD partitioning across WireGuard endpoints (a .fart peer cannot resolve
// .fart2 or .home) is enforced by rolodex at the source-IP level and is covered
// by the Rust tests in rolodex-dns; here we cover the LAN view, where every
// network TLD resolves.
func TestIntegrationPageOnNetworkDualHomesAndResolvesOnLAN(t *testing.T) {
	t.Parallel()

	realClient, dnsPort := initRolodexRealTest(t)

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}

	nm := initNetworkDB(t)
	pagesMgr := account.InitMockPagesManager()
	ingMock := &ingress.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       storage.InitBtrFSMock(),
		Systemd:       systemd.InitMockManager(),
		RolodexClient: realClient,
		IngressClient: ingMock,
		SettingsMgr:   settings,
		NetworkMgr:    nm,
		PagesMgr:      pagesMgr,
		BtrfsBasePath: t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })

	const lanIP = "192.168.122.50"
	ts.SetInternalIP(lanIP)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Boot-time network reconcile, exactly as main.go runs it: this is what
	// creates the default "home" network row (ensureDefaultNetwork) and its
	// rolodex scope. Without it a page (or package) targeting the default network
	// is rejected with `network "home" does not exist`.
	systemcontroller.ReconcileNetworks(ctx, systemcontroller.ReconcileNetworksConfig{
		NetworkMgr:       nm,
		Systemd:          systemd.InitMockManager(),
		NetworkStatePath: t.TempDir(),
		SettingsMgr:      settings,
		RolodexClient:    realClient,
	})

	// Create the fart network through the real path so the rolodex scope that
	// owns the fart TLD exists.
	if _, err := c.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// A page on the default network, and a page on fart.
	if _, err := c.CreatePage(ctx, "blog", "", "", "blog", account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage blog: %v", err)
	}
	if _, err := c.CreatePage(ctx, "secret", "", "", "secret", account.PageSourceArchive, "", "", "fart"); err != nil {
		t.Fatalf("CreatePage secret: %v", err)
	}

	const fartFQDN = "secret.fart."
	const homeFQDN = "blog.home."
	resolver := lanResolver(dnsPort)

	// The LAN/loopback view resolves the network-TLD page at the box's LAN IP.
	if addrs := resolveEventually(ctx, t, resolver, fartFQDN); !slices.Contains(addrs, lanIP) {
		t.Fatalf("expected %s to resolve to the LAN IP %s, got %v", fartFQDN, lanIP, addrs)
	}
	// ...and the default-network page still resolves in the home zone.
	if addrs := resolveEventually(ctx, t, resolver, homeFQDN); !slices.Contains(addrs, lanIP) {
		t.Fatalf("expected %s to resolve to %s, got %v", homeFQDN, lanIP, addrs)
	}

	// The overlay-facing scoped record exists for WireGuard peers (served by
	// source IP; not resolvable from this loopback view).
	scoped, err := realClient.ListScopedRecords(ctx, "fart", nil)
	if err != nil {
		t.Fatalf("ListScopedRecords: %v", err)
	}
	if findScopedRecord(scoped, fartFQDN, upstream.RecordTypeA) == nil {
		t.Fatalf("expected a scoped overlay record for %s, got %+v", fartFQDN, scoped)
	}

	// The fart page must never resolve under .home (the "resolves as .home" bug).
	if addrs, _ := resolver.LookupHost(ctx, "secret.home."); len(addrs) > 0 {
		t.Fatalf("network page must not resolve under .home, got %v", addrs)
	}

	// The ingress serves the page at its network FQDN, not the global one.
	hosts := map[string]bool{}
	for _, r := range ingMock.Routes {
		hosts[r.GetHostname()] = true
	}
	if !hosts["secret.fart"] {
		t.Fatalf("ingress must serve the page at secret.fart; routes: %v", hosts)
	}
	if hosts["secret.home"] {
		t.Fatalf("ingress must NOT serve the network page under the global dns_tld; routes: %v", hosts)
	}
	if !hosts["blog.home"] {
		t.Fatalf("default-network page must still be served at blog.home; routes: %v", hosts)
	}
}
