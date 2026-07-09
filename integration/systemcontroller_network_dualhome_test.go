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
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestIntegrationPackageDualHomedOnNetwork verifies that a package installed
// into a non-default WireGuard network is reachable from BOTH networks at the
// same time. The controller dual-homes its DNS via split-horizon:
//
//   - a SCOPED record under the network's TLD -> the box's overlay IP, served
//     to WireGuard peers, and
//   - a GLOBAL record under the same TLD -> the box's internal LAN IP, served
//     to loopback/LAN clients.
//
// Ingress already listens on all interfaces, so once each side resolves to an
// address it can route to (overlay over the tunnel, LAN IP on the local
// network) it reaches the same package. Before this, a network package resolved
// only to the overlay IP and was unreachable from the LAN. This drives the full
// HTTP install path (registerScopedPackageDNS -> RegisterPackageDNS), not just
// the standalone helpers the unit tests cover.
func TestIntegrationPackageDualHomedOnNetwork(t *testing.T) {
	t.Parallel()

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

	// A minimal HTTP package with a network port, installed into "fart".
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

	// A real network manager seeded with the non-default "fart" network
	// (TLD "fart", overlay address 10.65.0.1).
	nm := initNetworkDB(t)
	if _, err := nm.Create(&account.Network{
		Name: "fart", TLD: "fart", Subnet: "10.65.0.1/24", Address: "10.65.0.1/24",
		PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("seed network: %v", err)
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
		NetworkMgr:     nm,
	})
	t.Cleanup(func() { ts.Server.Close() })
	ts.SetInternalIP("192.168.122.50") // the box's LAN address

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	if err := c.InstallPackageInNetwork(context.TODO(), "webpkg", "1.0", packages.Responses{"port": "9191"}, "fart"); err != nil {
		t.Fatalf("InstallPackageInNetwork: %v", err)
	}

	const fqdn = "webpkg.local.fart."

	// Overlay-facing SCOPED record -> the box's overlay IP (WireGuard peers).
	scoped, err := rc.ListScopedRecords(context.Background(), "fart", nil)
	if err != nil {
		t.Fatalf("ListScopedRecords: %v", err)
	}
	if rec := findScopedRecord(scoped, fqdn, upstream.RecordTypeA); rec == nil || rec.Value != "10.65.0.1" {
		t.Fatalf("expected scoped A %s -> 10.65.0.1 (overlay), got %+v", fqdn, rec)
	}

	// LAN-facing GLOBAL record -> the box's internal LAN IP (local clients).
	global, err := rc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if rec := findScopedRecord(global, fqdn, upstream.RecordTypeA); rec == nil || rec.Value != "192.168.122.50" {
		t.Fatalf("expected global A %s -> 192.168.122.50 (LAN), got %+v", fqdn, rec)
	}

	// It must never leak into the global home zone (the original .home bug).
	if rec := findScopedRecord(global, "webpkg.local.home.", upstream.RecordTypeA); rec != nil {
		t.Fatalf("network package must not resolve as webpkg.local.home., got %+v", rec)
	}
}
