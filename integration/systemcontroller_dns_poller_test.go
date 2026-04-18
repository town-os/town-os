// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// dnsLifecycleHarness carries the moving parts a DNS lifecycle test needs
// to drive the real install/uninstall path AND the standalone
// Rebuild/Reconcile DNS helpers. RepositoryRoot is exposed explicitly so
// the helpers can be called without round-tripping through the server.
type dnsLifecycleHarness struct {
	client *systemcontroller.SystemdClient
	rolo   *rolodex.MockClient
	inst   packages.Installer
	rr     *packages.RepositoryRoot
}

// initDNSLifecycleTest wires a full install-capable systemcontroller with
// a local file://... repository and a MockClient rolodex. Each helper that
// follows drives the same instance so the DNS lifecycle (startup rebuild,
// install push, hourly drift repair, IP-change rebuild) stays observable
// end-to-end.
func initDNSLifecycleTest(t *testing.T) *dnsLifecycleHarness {
	t.Helper()
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

	// One package that exposes HTTP — enough for a single A record.
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
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "dnspkg")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile pkg: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		RolodexClient:  rc,
		SettingsMgr:    settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	return &dnsLifecycleHarness{client: c, rolo: rc, inst: inst, rr: rr}
}

// countARecordsFor returns how many A records exist for the given fqdn.
func countARecordsFor(t *testing.T, rc *rolodex.MockClient, fqdn string) int {
	t.Helper()
	records, err := rc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	n := 0
	for _, r := range records {
		if r.Name == fqdn && r.RecordType == upstream.RecordTypeA {
			n++
		}
	}
	return n
}

// TestIntegrationInstallRegistersDNS is the per-install contract: a
// single-package install must push exactly one A record for the package
// without going through a full rebuild. Anything that treats
// install/uninstall as "push only what changed" will regress here.
func TestIntegrationInstallRegistersDNS(t *testing.T) {
	t.Parallel()
	h := initDNSLifecycleTest(t)

	pkgFQDN := "dnspkg.local.home."
	if got := countARecordsFor(t, h.rolo, pkgFQDN); got != 0 {
		t.Fatalf("before install: %d A records for %s, want 0", got, pkgFQDN)
	}

	if err := h.client.InstallPackage(context.TODO(), "dnspkg", "1.0", packages.Responses{"port": "9091"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if got := countARecordsFor(t, h.rolo, pkgFQDN); got != 1 {
		t.Fatalf("after install: %d A records for %s, want 1", got, pkgFQDN)
	}
}

// TestIntegrationRebuildDNSWipesAndRebuilds pins the startup/IP-change
// contract: a full RebuildDNS pass must clear stale rolodex records that
// don't correspond to any installed package AND re-add every installed
// package's record under the current internal IP.
func TestIntegrationRebuildDNSWipesAndRebuilds(t *testing.T) {
	t.Parallel()
	h := initDNSLifecycleTest(t)

	if err := h.client.InstallPackage(context.TODO(), "dnspkg", "1.0", packages.Responses{"port": "9092"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Inject drift: a stale A record for a package that is not installed.
	// Simulates what a crashed uninstall or out-of-sync rolodex leaves behind.
	aType := upstream.RecordTypeA
	if err := h.rolo.AddRecord(context.Background(), &upstream.DnsRecord{
		Name:       "ghost.local.home.",
		RecordType: aType,
		Value:      "10.0.0.42",
		Ttl:        300,
	}); err != nil {
		t.Fatalf("seed ghost record: %v", err)
	}

	// Rebuild: mimic a systemcontroller restart. Uses the same helper the
	// production startup path calls.
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	if err := systemcontroller.RebuildDNS(context.Background(), systemcontroller.ReconcileDNSConfig{
		Client:         h.rolo,
		Installer:      h.inst,
		RepositoryRoot: h.rr,
		SettingsMgr:    settings,
		InternalIP:     "10.0.0.42",
	}); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}

	// Ghost record must be gone. Installed package record must still resolve.
	if got := countARecordsFor(t, h.rolo, "ghost.local.home."); got != 0 {
		t.Fatalf("after rebuild: ghost record still present (%d copies)", got)
	}
	if got := countARecordsFor(t, h.rolo, "dnspkg.local.home."); got != 1 {
		t.Fatalf("after rebuild: dnspkg A records = %d, want 1", got)
	}
}

// TestIntegrationReconcileDNSHourlyRepairsDrift pins the drift-repair
// contract for the hourly poller: a stale rolodex record is removed, an
// installed package whose record somehow got dropped is re-added, and a
// record that is already correct is left strictly alone (no mutation).
func TestIntegrationReconcileDNSHourlyRepairsDrift(t *testing.T) {
	t.Parallel()
	h := initDNSLifecycleTest(t)

	if err := h.client.InstallPackage(context.TODO(), "dnspkg", "1.0", packages.Responses{"port": "9093"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Drift: drop the correct record AND add a ghost.
	aType := upstream.RecordTypeA
	if _, err := h.rolo.RemoveRecord(context.Background(), "dnspkg.local.home.", &upstream.RemoveRecordOptions{RecordType: &aType}); err != nil {
		t.Fatalf("drop dnspkg record: %v", err)
	}
	if err := h.rolo.AddRecord(context.Background(), &upstream.DnsRecord{
		Name:       "ghost.local.home.",
		RecordType: aType,
		Value:      "10.0.0.42",
		Ttl:        300,
	}); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}

	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	if err := systemcontroller.ReconcileDNS(context.Background(), systemcontroller.ReconcileDNSConfig{
		Client:         h.rolo,
		Installer:      h.inst,
		RepositoryRoot: h.rr,
		SettingsMgr:    settings,
		InternalIP:     "10.0.0.42",
	}); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	if got := countARecordsFor(t, h.rolo, "dnspkg.local.home."); got != 1 {
		t.Fatalf("after reconcile: dnspkg record = %d, want 1 (re-added)", got)
	}
	if got := countARecordsFor(t, h.rolo, "ghost.local.home."); got != 0 {
		t.Fatalf("after reconcile: ghost record = %d, want 0 (removed)", got)
	}

	// A second reconcile with no drift must make ZERO mutations. This is
	// the contract that makes the hourly poller cheap: polling a
	// steady-state system never touches rolodex.
	preIdempotent := len(h.rolo.GetCalls())
	if err := systemcontroller.ReconcileDNS(context.Background(), systemcontroller.ReconcileDNSConfig{
		Client:         h.rolo,
		Installer:      h.inst,
		RepositoryRoot: h.rr,
		SettingsMgr:    settings,
		InternalIP:     "10.0.0.42",
	}); err != nil {
		t.Fatalf("second ReconcileDNS: %v", err)
	}
	var added, removed int
	for _, call := range h.rolo.GetCalls()[preIdempotent:] {
		switch call.Method {
		case "AddRecord":
			added++
		case "RemoveRecord":
			removed++
		}
	}
	if added != 0 || removed != 0 {
		t.Fatalf("idempotent reconcile mutated rolodex: add=%d remove=%d", added, removed)
	}
}
