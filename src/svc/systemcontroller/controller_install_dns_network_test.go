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
// manager, an installer, and a fixed internal IP, for exercising the package
// DNS-plumbing paths without the full install machinery.
func dnsNetworkHandler(nm account.NetworkManager, rc rolodex.Client, inst packages.Installer) *SystemControllerHandlers {
	sb := &serverBase{ServerConfig: ServerConfig{NetworkMgr: nm, RolodexClient: rc, Installer: inst}}
	sb.internalIP.Store("192.168.1.10")
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

// seedNetwork returns a mock network manager holding a single non-default
// network with the given name and TLD, plus the always-present default network.
func seedNetwork(t *testing.T, name, tld, addr string) *account.MockNetworkManager {
	t.Helper()
	nm := account.InitMockNetworkManager()
	n := &account.Network{Name: name, TLD: tld, Subnet: addr, Address: addr, PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	if _, err := nm.Create(n); err != nil {
		t.Fatalf("seed network %q: %v", name, err)
	}
	return nm
}

// A package installed into the default network is plumbed as a global record in
// the home zone and gets no scoped record.
func TestRegisterPackageDNSForNetworkDefaultUsesGlobalHomeZone(t *testing.T) {
	nm := account.InitMockNetworkManager()
	mc := &rolodex.MockClient{}
	s := dnsNetworkHandler(nm, mc, nil)

	s.registerPackageDNSForNetwork(context.Background(), account.DefaultNetworkName, "repo-a", "nginx", nil)

	if len(mc.ScopedRecords) != 0 {
		t.Fatalf("default network must not create scoped records, got %+v", mc.ScopedRecords)
	}
	if !hasGlobalRecord(mc, "nginx.repo-a.home.") {
		t.Fatalf("expected global record nginx.repo-a.home., got %v", globalNames(mc))
	}
}

// A package installed into a non-default network is plumbed as a scoped record
// under that network's TLD and MUST NOT land in the global home zone. This is
// the regression: a gitea instance on the "fart" network was resolving as
// gitea.default.home instead of gitea.default.fart.
func TestRegisterPackageDNSForNetworkNonDefaultUsesScopedTLDNotHome(t *testing.T) {
	nm := seedNetwork(t, "fart", "fart", "10.65.0.1/24")
	mc := &rolodex.MockClient{}
	s := dnsNetworkHandler(nm, mc, nil)

	s.registerPackageDNSForNetwork(context.Background(), "fart", "repo-a", "gitea", nil)

	// The scoped record lives under the network's TLD in the network's scope.
	scoped := mc.ScopedRecords["fart"]
	if !hasRecord(scoped, "gitea.repo-a.fart.") {
		t.Fatalf("expected scoped record gitea.repo-a.fart. in scope fart, got %v", recordNames(scoped))
	}
	// Crucially, nothing is written to the global home zone.
	if len(mc.Records) != 0 {
		t.Fatalf("non-default network must not create global records, got %v", globalNames(mc))
	}
	if hasGlobalRecord(mc, "gitea.repo-a.home.") {
		t.Fatal("package on the fart network must not resolve as gitea.repo-a.home.")
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

func globalNames(mc *rolodex.MockClient) string { return recordNames(mc.Records) }

func recordNames(recs []*upstream.DnsRecord) string {
	names := make([]string, 0, len(recs))
	for _, r := range recs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
}
