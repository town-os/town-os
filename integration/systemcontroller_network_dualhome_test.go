// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// lanResolver builds a net.Resolver that queries the real rolodex container over
// its DNS port on the loopback address — the LAN/local view (no WireGuard source
// address), which is exactly the client that used to get NXDOMAIN for a
// non-default-network package.
func lanResolver(dnsPort string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", rolodex.DNSLoopback+":"+dnsPort)
		},
	}
}

// resolveEventually polls the resolver until the name resolves or ctx expires,
// returning the addresses. rolodex needs a moment after a write before the
// answer is served, so we retry rather than assume the first query wins.
func resolveEventually(ctx context.Context, t *testing.T, r *net.Resolver, name string) []string {
	t.Helper()
	var addrs []string
	var err error
	for ctx.Err() == nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		addrs, err = r.LookupHost(lookupCtx, name)
		cancel()
		if err == nil && len(addrs) > 0 {
			return addrs
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("LookupHost(%s) never resolved: %v", name, err)
	return nil
}

// TestIntegrationPackageDualHomedOnNetwork verifies, against REAL rolodex and a
// live resolver, that a package installed into a non-default WireGuard network
// resolves from the LAN. The controller dual-homes DNS: a scoped record under
// the network TLD serves overlay peers, and a GLOBAL record under the same TLD
// serves loopback/LAN clients. A bare global A record resolves on the LAN with no
// authoritative zone (rolodex's LAN->owning-scope fallback treats the scope-owned
// TLD as authoritative for LAN sources), so no global SOA/NS zone is published.
// The original bug was that the box never wrote the LAN A record at all. This
// test drives the real HTTP install path and then queries the live resolver, and
// also exercises the boot rebuild (RebuildNetworkDNS) that restores LAN
// resolution for an already-installed package after a restart.
//
// TLD partitioning across WireGuard endpoints (a .fart peer cannot resolve .fart2
// or .home) is validated at the rolodex level, where the query source IP is
// controllable — see the Rust tests in rolodex-dns/src/dns_server.rs
// (test_wg_peer_partition_hides_sibling_tld_and_home). This integration test
// covers the LAN view, where every network TLD is resolvable.
func TestIntegrationPackageDualHomedOnNetwork(t *testing.T) {
	t.Parallel()

	realClient, dnsPort := initRolodexRealTest(t)

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgYAML := `image: nginx:1.0
description: "test pkg"
network:
  external:
    "@port@": "80"
volumes: {}
questions:
  port:
    query: "port?"
    type: port
    default: "8080"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "webpkg")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile pkg: %v", err)
	}

	nm := initNetworkDB(t)
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:          mock,
		RepositoryRoot:   rr,
		Installer:        inst,
		Systemd:          sd,
		RolodexClient:    realClient,
		SettingsMgr:      settings,
		NetworkMgr:       nm,
		NetworkStatePath: t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })
	const lanIP = "192.168.122.50" // the box's LAN address (the record value)
	ts.SetInternalIP(lanIP)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Create the fart network through the real path → applyNetworkTransport
	// creates the rolodex scope that owns the fart TLD and publishes the scoped
	// apex.
	if _, err := c.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// Install the package into fart → registerScopedPackageDNS publishes the
	// scoped overlay record and the LAN-facing global record.
	if err := c.InstallPackageInNetwork(ctx, "webpkg", "1.0", packages.Responses{"port": "9191"}, "fart"); err != nil {
		t.Fatalf("InstallPackageInNetwork: %v", err)
	}

	const fqdn = "webpkg.local.fart."
	resolver := lanResolver(dnsPort)

	// The LAN/loopback view resolves the network-TLD package — the exact query
	// that returned NXDOMAIN before the LAN-facing record was written.
	if addrs := resolveEventually(ctx, t, resolver, fqdn); !slices.Contains(addrs, lanIP) {
		t.Fatalf("expected %s to resolve to %s (LAN) via the live resolver, got %v", fqdn, lanIP, addrs)
	}

	// The overlay-facing scoped record still exists in rolodex (served to peers
	// by source IP; not resolvable from this loopback view).
	scoped, err := realClient.ListScopedRecords(ctx, "fart", nil)
	if err != nil {
		t.Fatalf("ListScopedRecords: %v", err)
	}
	if findScopedRecord(scoped, fqdn, upstream.RecordTypeA) == nil {
		t.Fatalf("expected scoped overlay record %s, got %+v", fqdn, scoped)
	}

	// It must never leak into the global home zone (the original .home bug): a
	// .home lookup must not return the package's address.
	if homeAddrs, _ := resolver.LookupHost(ctx, "webpkg.local.home."); slices.Contains(homeAddrs, lanIP) {
		t.Fatalf("network package must not resolve under .home, got %v", homeAddrs)
	}

	// Boot rebuild: simulate a systemcontroller restart. RebuildNetworkDNS
	// re-registers the LAN-facing global records from the install records, so an
	// already-installed package keeps resolving on the LAN (the scoped records
	// persist in rolodex independently).
	if err := systemcontroller.RebuildNetworkDNS(ctx, systemcontroller.ReconcileDNSConfig{
		Client:         realClient,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		NetworkMgr:     nm,
		InternalIP:     lanIP,
	}); err != nil {
		t.Fatalf("RebuildNetworkDNS: %v", err)
	}

	if err := realClient.FlushDnsCache(ctx); err != nil {
		t.Logf("FlushDnsCache: %v", err)
	}
	if addrs := resolveEventually(ctx, t, resolver, fqdn); !slices.Contains(addrs, lanIP) {
		t.Fatalf("after boot rebuild, expected %s to still resolve to %s, got %v", fqdn, lanIP, addrs)
	}
}

// TestIntegrationAllNetworkTLDsResolveOnLAN verifies, against REAL rolodex and a
// live resolver, that packages installed into DIFFERENT WireGuard networks are
// ALL resolvable from the LAN under their own TLDs — .fart AND .fart2 at once.
// This is the "all TLDs available over the LAN" guarantee: the LAN view is never
// partitioned (only WireGuard peers are), so a single LAN client resolves every
// network's packages. Partitioning across WG endpoints — a .fart peer cannot see
// .fart2 or .home — is covered by the rolodex Rust tests where the query source
// IP can be forged.
func TestIntegrationAllNetworkTLDsResolveOnLAN(t *testing.T) {
	t.Parallel()

	realClient, dnsPort := initRolodexRealTest(t)

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgYAML := func(port string) string {
		return `image: nginx:1.0
description: "test pkg"
network:
  external:
    "@port@": "80"
volumes: {}
questions:
  port:
    query: "port?"
    type: port
    default: "` + port + `"
`
	}
	for _, p := range []struct{ name, port string }{{"webpkg", "8080"}, {"wikipkg", "8081"}} {
		pkgDir := filepath.Join(dir, "local", packages.PackagesDir, p.name)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll pkgDir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML(p.port)), 0600); err != nil {
			t.Fatalf("WriteFile pkg %s: %v", p.name, err)
		}
	}

	nm := initNetworkDB(t)
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:          mock,
		RepositoryRoot:   rr,
		Installer:        inst,
		Systemd:          sd,
		RolodexClient:    realClient,
		SettingsMgr:      settings,
		NetworkMgr:       nm,
		NetworkStatePath: t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })
	const lanIP = "192.168.122.50"
	ts.SetInternalIP(lanIP)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Two separate WireGuard networks, each owning its own TLD.
	if _, err := c.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork fart: %v", err)
	}
	if _, err := c.CreateNetwork(ctx, "fart2", "fart2"); err != nil {
		t.Fatalf("CreateNetwork fart2: %v", err)
	}
	if err := c.InstallPackageInNetwork(ctx, "webpkg", "1.0", packages.Responses{"port": "9191"}, "fart"); err != nil {
		t.Fatalf("InstallPackageInNetwork webpkg/fart: %v", err)
	}
	if err := c.InstallPackageInNetwork(ctx, "wikipkg", "1.0", packages.Responses{"port": "9192"}, "fart2"); err != nil {
		t.Fatalf("InstallPackageInNetwork wikipkg/fart2: %v", err)
	}

	// Re-register both networks' LAN-facing records at the box's LAN IP via the
	// boot-rebuild path. This pins the record value deterministically (the
	// install-time value is whatever GetInternalIP returned then, which the test
	// harness's internal-IP refresh can move off the pinned lanIP) and exercises
	// RebuildNetworkDNS across two networks at once.
	if err := systemcontroller.RebuildNetworkDNS(ctx, systemcontroller.ReconcileDNSConfig{
		Client:         realClient,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		NetworkMgr:     nm,
		InternalIP:     lanIP,
	}); err != nil {
		t.Fatalf("RebuildNetworkDNS: %v", err)
	}
	if err := realClient.FlushDnsCache(ctx); err != nil {
		t.Logf("FlushDnsCache: %v", err)
	}

	resolver := lanResolver(dnsPort)

	// A single LAN client resolves BOTH network TLDs at the box's LAN IP — the LAN
	// view is never partitioned, so every network's packages are visible.
	for _, tc := range []struct{ fqdn, network string }{
		{"webpkg.local.fart.", "fart"},
		{"wikipkg.local.fart2.", "fart2"},
	} {
		if addrs := resolveEventually(ctx, t, resolver, tc.fqdn); !slices.Contains(addrs, lanIP) {
			t.Fatalf("expected %s (network %s) to resolve to %s on the LAN, got %v", tc.fqdn, tc.network, lanIP, addrs)
		}
	}
}
