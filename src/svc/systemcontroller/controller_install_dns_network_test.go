// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"slices"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
)

// dnsNetworkHandler builds a handler wired with a rolodex mock, a network
// manager, and a fixed internal IP, for exercising the package DNS-plumbing
// paths without the full install machinery.
func dnsNetworkHandler(nm account.NetworkManager, rc rolodex.Client) *SystemControllerHandlers {
	sb := &serverBase{ServerConfig: ServerConfig{NetworkMgr: nm, RolodexClient: rc}}
	sb.internalIP.Store("192.168.1.10")
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

// seedNetwork returns a mock network manager holding the non-default "fart"
// network (TLD "fart") used across the DNS/cert network tests.
func seedNetwork(t *testing.T) *account.MockNetworkManager {
	t.Helper()
	nm := account.InitMockNetworkManager()
	n := &account.Network{Name: "fart", TLD: "fart", Subnet: "10.65.0.1/24", Address: "10.65.0.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	if _, err := nm.Create(n); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	return nm
}

// A package installed into the default network is plumbed as a global record in
// the home zone and gets no scoped record.
func TestRegisterPackageDNSForNetworkDefaultUsesGlobalHomeZone(t *testing.T) {
	nm := account.InitMockNetworkManager()
	mc := &rolodex.MockClient{}
	s := dnsNetworkHandler(nm, mc)

	s.registerPackageDNSForNetwork(context.Background(), account.DefaultNetworkName, "repo-a", "nginx", nil)

	if len(mc.ScopedRecords) != 0 {
		t.Fatalf("default network must not create scoped records, got %+v", mc.ScopedRecords)
	}
	if !hasGlobalRecord(mc, "nginx.repo-a.home.") {
		t.Fatalf("expected global record nginx.repo-a.home., got %v", globalNames(mc))
	}
}

// A package installed into a non-default network is dual-homed so it is
// reachable from BOTH the WireGuard overlay and the local (LAN) network:
//   - a SCOPED record under the network's TLD pointing at the overlay IP
//     (served to overlay peers), and
//   - a GLOBAL record under the same network TLD pointing at the box's LAN IP
//     (served to loopback/LAN clients).
// It MUST NOT land in the global home zone (the original regression: a gitea
// instance on "fart" resolving as gitea.default.home instead of .fart).
func TestRegisterPackageDNSForNetworkNonDefaultDualHomes(t *testing.T) {
	nm := seedNetwork(t)
	mc := &rolodex.MockClient{}
	s := dnsNetworkHandler(nm, mc)

	s.registerPackageDNSForNetwork(context.Background(), "fart", "repo-a", "gitea", nil)

	// Overlay-facing scoped record: the network's TLD in the network's scope,
	// pointing at the box's overlay address (10.65.0.1 from seedNetwork).
	scoped := mc.ScopedRecords["fart"]
	rec := recordByType(scoped, "gitea.repo-a.fart.")
	if rec == nil {
		t.Fatalf("expected scoped A record gitea.repo-a.fart. in scope fart, got %v", recordNames(scoped))
	}
	if rec.Value != "10.65.0.1" {
		t.Fatalf("scoped record must point at the overlay IP 10.65.0.1, got %q", rec.Value)
	}

	// LAN-facing global record: the same FQDN under the network TLD, pointing at
	// the box's internal LAN IP (192.168.1.10 from dnsNetworkHandler). A bare
	// global A record resolves on the LAN with no authoritative zone — rolodex's
	// LAN->owning-scope fallback treats the network TLD (owned by the scope) as
	// authoritative for LAN sources — so no global SOA/NS zone is published.
	global := recordByType(mc.Records, "gitea.repo-a.fart.")
	if global == nil {
		t.Fatalf("expected global A record gitea.repo-a.fart. for LAN clients, got %v", globalNames(mc))
	}
	if global.Value != "192.168.1.10" {
		t.Fatalf("global record must point at the internal LAN IP 192.168.1.10, got %q", global.Value)
	}

	// No global authoritative zone for the network TLD: the scope owns it, and a
	// duplicate global SOA/NS apex would only shadow the scoped apex.
	if hasAuthZone(mc, "fart.") {
		t.Fatalf("must not publish a global authoritative zone fart.; got %v", mc.AuthZones)
	}

	// The scope owning the network TLD is what makes the LAN fallback authoritative
	// and partitions the TLD from foreign WireGuard peers.
	if !hasScope(mc, "fart", "fart.") {
		t.Fatalf("expected a rolodex scope owning fart. (home_domain), got %v", scopeNames(mc))
	}

	// Crucially, it still never leaks into the global HOME zone.
	if hasGlobalRecord(mc, "gitea.repo-a.home.") {
		t.Fatal("package on the fart network must not resolve as gitea.repo-a.home.")
	}
}

// hasAuthZone reports whether the mock recorded a global authoritative zone.
func hasAuthZone(mc *rolodex.MockClient, zone string) bool {
	return slices.Contains(mc.AuthZones, zone)
}

// hasScope reports whether the mock recorded a network scope with the given name
// and home_domain (the scope's implicit owned TLD).
func hasScope(mc *rolodex.MockClient, name, homeDomain string) bool {
	for _, s := range mc.Scopes {
		if s.Name == name && s.HomeDomain == homeDomain {
			return true
		}
	}
	return false
}

// scopeNames renders the recorded scope names for failure messages.
func scopeNames(mc *rolodex.MockClient) string {
	names := make([]string, 0, len(mc.Scopes))
	for _, s := range mc.Scopes {
		names = append(names, s.Name+"="+s.HomeDomain)
	}
	return strings.Join(names, ",")
}

// unregisterScopedPackageDNS is the inverse of the dual-homing register: it must
// remove BOTH the scoped overlay record and the global LAN record so neither
// outlives the package.
func TestUnregisterScopedPackageDNSRemovesBothHomings(t *testing.T) {
	nm := seedNetwork(t)
	mc := &rolodex.MockClient{}
	s := dnsNetworkHandler(nm, mc)

	s.registerPackageDNSForNetwork(context.Background(), "fart", "repo-a", "gitea", nil)
	if !hasRecord(mc.ScopedRecords["fart"], "gitea.repo-a.fart.") || !hasGlobalRecord(mc, "gitea.repo-a.fart.") {
		t.Fatalf("precondition: expected both scoped and global records after register")
	}

	s.unregisterScopedPackageDNS(context.Background(), "fart", "repo-a", "gitea", nil)

	if hasRecord(mc.ScopedRecords["fart"], "gitea.repo-a.fart.") {
		t.Fatalf("scoped record should be removed, got %v", recordNames(mc.ScopedRecords["fart"]))
	}
	if hasGlobalRecord(mc, "gitea.repo-a.fart.") {
		t.Fatalf("global record should be removed, got %v", globalNames(mc))
	}
}

// collectInstalledDNSInfo builds the desired global-zone set. Packages assigned
// to a non-default network must be excluded so they never enter the home zone.
func TestCollectInstalledDNSInfoExcludesNonDefaultNetworkPackages(t *testing.T) {
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo-a", Name: "nginx", Version: "1.0"},
		{Repo: "repo-a", Name: "gitea", Version: "2.0"},
	}
	// nginx stays in the default (home) zone; gitea belongs to the fart network.
	if err := inst.SaveNetwork("repo-a", "nginx", account.DefaultNetworkName); err != nil {
		t.Fatalf("save nginx network: %v", err)
	}
	if err := inst.SaveNetwork("repo-a", "gitea", "fart"); err != nil {
		t.Fatalf("save gitea network: %v", err)
	}

	got := collectInstalledDNSInfo(inst, nil, "home")
	if len(got) != 1 {
		t.Fatalf("expected 1 home-zone package, got %d: %+v", len(got), got)
	}
	if got[0].Name != "nginx" {
		t.Fatalf("expected only nginx in the home zone, got %q", got[0].Name)
	}
}

// ReconcileDNS must delete a stale global home-zone record left behind by the
// old always-home install path once the package is known to belong to a
// non-default network. This proves the fix survives reboot/hourly reconcile.
func TestReconcileDNSRemovesStaleGlobalHomeRecordForNetworkedPackage(t *testing.T) {
	mc := &rolodex.MockClient{
		AuthZones: []string{"home."},
		Records: []*upstream.DnsRecord{
			{Name: "gitea.repo-a.home.", RecordType: upstream.RecordTypeA, Value: "192.168.1.10", Ttl: 300},
		},
	}
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{{Repo: "repo-a", Name: "gitea", Version: "2.0"}}
	if err := inst.SaveNetwork("repo-a", "gitea", "fart"); err != nil {
		t.Fatalf("save gitea network: %v", err)
	}

	err := ReconcileDNS(context.Background(), ReconcileDNSConfig{
		Client:     mc,
		Installer:  inst,
		InternalIP: "192.168.1.10",
	})
	if err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	if hasGlobalRecord(mc, "gitea.repo-a.home.") {
		t.Fatalf("stale global home record should have been removed, got %v", globalNames(mc))
	}
}

// RebuildNetworkDNS (boot path) must register each non-default network package's
// LAN-facing global record at the box's LAN IP, while leaving default-network
// packages out of the network TLD. This is what makes an existing fart-network
// install resolve on the LAN after a restart — the scoped records persist, and
// the LAN half is rebuilt here. It publishes NO global authoritative zone: a bare
// global A record resolves on the LAN via rolodex's LAN->owning-scope fallback.
func TestRebuildNetworkDNSRegistersLANRecords(t *testing.T) {
	nm := seedNetwork(t) // fart network, TLD "fart"
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "default", Name: "gitea", Version: "2.0"},
		{Repo: "default", Name: "nginx", Version: "1.0"},
	}
	// gitea lives on fart; nginx stays on the default (home) network.
	if err := inst.SaveNetwork("default", "gitea", "fart"); err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}

	mc := &rolodex.MockClient{}
	if err := RebuildNetworkDNS(context.Background(), ReconcileDNSConfig{
		Client:     mc,
		Installer:  inst,
		NetworkMgr: nm,
		InternalIP: "192.168.1.50",
	}); err != nil {
		t.Fatalf("RebuildNetworkDNS: %v", err)
	}

	// No global authoritative zone for the network TLD — the scope owns it and the
	// bare global A record below is LAN-resolvable via the fallback.
	if hasAuthZone(mc, "fart.") {
		t.Fatalf("must not publish a global authoritative zone fart.; got %v", mc.AuthZones)
	}
	// gitea's LAN-facing A record under the network TLD at the box's LAN IP.
	rec := recordByType(mc.Records, "gitea.default.fart.")
	if rec == nil || rec.Value != "192.168.1.50" {
		t.Fatalf("expected gitea.default.fart. A 192.168.1.50, got %v / %v", rec, globalNames(mc))
	}
	// nginx is on the default network — it must NOT appear under the fart TLD.
	if recordByType(mc.Records, "nginx.default.fart.") != nil {
		t.Fatal("default-network nginx must not be registered under the fart TLD")
	}
	// The global home zone is RebuildDNS's responsibility, not this function's.
	if hasAuthZone(mc, "home.") {
		t.Fatal("RebuildNetworkDNS must not touch the global home zone")
	}
}

// onInternalIPChange must re-pin BOTH the global home zone (via RebuildDNS) AND
// each non-default-network package's LAN-facing global record (via
// RebuildNetworkDNS) to the new IP. Before this wiring a dual-homed package's LAN
// record kept the stale IP after the box's address changed at runtime until the
// next reboot (RebuildDNS's home-zone reconcile excludes networked packages).
func TestOnInternalIPChangeRepinsNetworkPackageRecords(t *testing.T) {
	nm := seedNetwork(t) // fart network, TLD "fart"
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{
		{Repo: "default", Name: "gitea", Version: "2.0"},
		{Repo: "default", Name: "nginx", Version: "1.0"},
	}
	// gitea lives on the fart network; nginx stays on the default home network.
	if err := inst.SaveNetwork("default", "gitea", "fart"); err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}

	mc := &rolodex.MockClient{}
	s := &serverBase{ServerConfig: ServerConfig{
		RolodexClient: mc,
		Installer:     inst,
		NetworkMgr:    nm,
	}}
	s.internalIPv6.Store("") // pin IPv6 empty so GetInternalIPv6 does no live discovery

	s.onInternalIPChange(context.Background(), "192.168.1.10", "192.168.1.99")

	// The fart-network package's LAN record is re-pinned under its network TLD.
	if rec := recordByType(mc.Records, "gitea.default.fart."); rec == nil || rec.Value != "192.168.1.99" {
		t.Fatalf("expected gitea.default.fart. re-pinned to 192.168.1.99, got %+v", rec)
	}
	// The home-network package is re-pinned in the global home zone.
	if rec := recordByType(mc.Records, "nginx.default.home."); rec == nil || rec.Value != "192.168.1.99" {
		t.Fatalf("expected nginx.default.home. re-pinned to 192.168.1.99, got %+v", rec)
	}
}

func hasGlobalRecord(mc *rolodex.MockClient, name string) bool {
	return hasRecord(mc.Records, name)
}

func hasRecord(recs []*upstream.DnsRecord, name string) bool {
	for _, r := range recs {
		if r.Name == name {
			return true
		}
	}
	return false
}

// recordByType returns the first A record matching name, or nil.
func recordByType(recs []*upstream.DnsRecord, name string) *upstream.DnsRecord {
	for _, r := range recs {
		if r.Name == name && r.RecordType == upstream.RecordTypeA {
			return r
		}
	}
	return nil
}

func globalNames(mc *rolodex.MockClient) string { return recordNames(mc.Records) }

func recordNames(recs []*upstream.DnsRecord) string {
	names := make([]string, 0, len(recs))
	for _, r := range recs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
}
