// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// TestIntegrationIngressInstallRolodex drives a full install→uninstall cycle for
// an HTTP package and asserts the two control planes the systemcontroller
// programs in tandem: rolodex DNS (an A record + the DANE _443 TLSA) and the
// shared :443 ingress (a route to the service container, terminating TLS with
// the issued local-CA leaf). Uninstall must withdraw the ingress route.
func TestIntegrationIngressInstallRolodex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	btrfsBase := filepath.Join(dir, "btrfs")
	networkStateDir := filepath.Join(dir, "network-state")
	for _, d := range []string{btrfsBase, networkStateDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	repoData, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), repoData, 0o600); err != nil {
		t.Fatal(err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatal(err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgYAML := `image: nginx:1.0
description: "ingress + rolodex test"
supplies: ["http"]
network:
  internal:
    http: "@httpport@"
volumes: {}
questions:
  httpport:
    query: "port?"
    type: port
    default: "3000"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "webpkg")
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	inst := packages.NewInstallManager(dir)
	rolMock := &rolodex.MockClient{}
	ingMock := &ingress.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:          storage.InitBtrFSMock(),
		RepositoryRoot:   rr,
		Installer:        inst,
		Systemd:          systemd.InitMockManager(),
		RolodexClient:    rolMock,
		IngressClient:    ingMock,
		SettingsMgr:      settings,
		BtrfsBasePath:    btrfsBase,
		NetworkStatePath: networkStateDir,
		TLSCA:            ca,
	})
	ts.SetInternalIP("192.168.10.50")
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatal(err)
	}

	if err := c.InstallPackage(context.TODO(), "webpkg", "1.0", packages.Responses{"httpport": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	const fqdn = "webpkg.local.home"

	// --- rolodex: A record + DANE TLSA on :443 for the package FQDN ---
	if !hasRolodexRecord(rolMock, upstream.RecordTypeA, fqdn+".") {
		t.Fatalf("rolodex missing A record for %s.; A records: %v", fqdn, rolodexRecordNames(rolMock, upstream.RecordTypeA))
	}
	tlsaName := "_443._tcp." + fqdn + "."
	var tlsaVal string
	for _, r := range rolMock.Records {
		if r.RecordType == upstream.RecordTypeTLSA && r.Name == tlsaName {
			tlsaVal = r.Value
		}
	}
	if tlsaVal == "" {
		t.Fatalf("rolodex missing TLSA at %s; records: %+v", tlsaName, rolMock.Records)
	}
	if !strings.HasPrefix(tlsaVal, "3 1 1 ") {
		t.Fatalf("TLSA must be DANE-EE/SPKI/SHA-256 (3 1 1 ...), got %q", tlsaVal)
	}

	// --- ingress: a route for the FQDN → service container, pinning the leaf ---
	route := ingMock.RouteFor(fqdn)
	if route == nil {
		t.Fatalf("ingress not programmed with a route for %s; routes: %+v", fqdn, ingMock.Routes)
	}
	if !strings.HasSuffix(route.GetBackend(), ":8080") {
		t.Fatalf("ingress backend should target the service container on :8080, got %q", route.GetBackend())
	}
	if route.GetCertDir() == "" || route.GetAcme() {
		t.Fatalf("internal ingress route must pin a local leaf (cert dir set, not ACME): %+v", route)
	}

	// --- uninstall withdraws the ingress route ---
	if err := c.UninstallPackage(context.TODO(), "local", "webpkg", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}
	if r := ingMock.RouteFor(fqdn); r != nil {
		t.Fatalf("ingress route for %s still present after uninstall: %+v", fqdn, r)
	}
}

func hasRolodexRecord(m *rolodex.MockClient, rt upstream.RecordType, name string) bool {
	for _, r := range m.Records {
		if r.RecordType == rt && r.Name == name {
			return true
		}
	}
	return false
}

func rolodexRecordNames(m *rolodex.MockClient, rt upstream.RecordType) []string {
	var out []string
	for _, r := range m.Records {
		if r.RecordType == rt {
			out = append(out, r.Name)
		}
	}
	return out
}
