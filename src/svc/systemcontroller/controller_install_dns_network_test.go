// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
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
	rec := recordByType(scoped, "gitea.repo-a.fart.", upstream.RecordTypeA)
	if rec == nil {
		t.Fatalf("expected scoped A record gitea.repo-a.fart. in scope fart, got %v", recordNames(scoped))
	}
	if rec.Value != "10.65.0.1" {
		t.Fatalf("scoped record must point at the overlay IP 10.65.0.1, got %q", rec.Value)
	}

	// LAN-facing global record: the same FQDN under the network TLD, pointing at
	// the box's internal LAN IP (192.168.1.10 from dnsNetworkHandler).
	global := recordByType(mc.Records, "gitea.repo-a.fart.", upstream.RecordTypeA)
	if global == nil {
		t.Fatalf("expected global A record gitea.repo-a.fart. for LAN clients, got %v", globalNames(mc))
	}
	if global.Value != "192.168.1.10" {
		t.Fatalf("global record must point at the internal LAN IP 192.168.1.10, got %q", global.Value)
	}

	// Crucially, it still never leaks into the global HOME zone.
	if hasGlobalRecord(mc, "gitea.repo-a.home.") {
		t.Fatal("package on the fart network must not resolve as gitea.repo-a.home.")
	}
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

// recordByType returns the first record matching both name and type, or nil.
func recordByType(recs []*upstream.DnsRecord, name string, rt upstream.RecordType) *upstream.DnsRecord {
	for _, r := range recs {
		if r.Name == name && r.RecordType == rt {
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
