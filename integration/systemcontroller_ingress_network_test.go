// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gitea.com/town-os/town-os/src/ingress"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// routeHostnames returns the hostnames of the ingress mock's current route set.
func routeHostnames(routes []*ingresspb.Route) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.GetHostname())
	}
	return out
}

// routeFor returns the route with the given hostname, or nil.
func routeFor(routes []*ingresspb.Route, host string) *ingresspb.Route {
	for _, r := range routes {
		if r.GetHostname() == host {
			return r
		}
	}
	return nil
}

// readNetworkState decodes the per-package network state file.
func readNetworkState(t *testing.T, stateDir, repo, pkg, version string) networkcontroller.PackageNetworkState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, repo+"-"+pkg+"-"+version+".json"))
	if err != nil {
		t.Fatalf("read network state: %v", err)
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}
	return st
}

// leafDNSNames parses the package's issued leaf and returns its DNS SANs.
func leafDNSNames(t *testing.T, btrfsBase, repo, pkg, version string) []string {
	t.Helper()
	certPath := filepath.Join(btrfsBase, "tls", "leaves", repo, pkg, version, "cert.pem")
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read leaf cert: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("leaf cert PEM decode failed")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return parsed.DNSNames
}

// TestIntegrationIngressServesPackageUnderNetworkTLD is the end-to-end
// reproduction of the reported bug: gitea.default.fart resolved on the LAN but
// was never served.
//
// The ingress itself was never at fault — it publishes :443 on 0.0.0.0 (LAN +
// WireGuard + loopback) and SNI-selects its vhosts. The bug was that it NAMED
// the vhost from the global dns_tld, so it rendered a `webpkg.local.home` site
// that nothing dialed and attached a leaf that was only valid for
// `webpkg.local.fart`. A LAN client sending SNI webpkg.local.fart matched no
// site and fell through to the UI.
//
// This asserts the three names that must now agree: the ingress route hostname,
// the network state file's FQDN, and the leaf certificate's SAN.
func TestIntegrationIngressServesPackageUnderNetworkTLD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	btrfsBase := t.TempDir()
	stateDir := t.TempDir()

	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0o600); err != nil {
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
description: "ingress network TLD test"
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
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0o600); err != nil {
		t.Fatalf("WriteFile pkg: %v", err)
	}

	nm := initNetworkDB(t)
	inst := packages.NewInstallManager(dir)
	ingMock := &ingress.MockClient{}
	rolMock := &rolodex.MockClient{}
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
		NetworkMgr:       nm,
		BtrfsBasePath:    btrfsBase,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
	})
	t.Cleanup(func() { ts.Server.Close() })
	const lanIP = "192.168.122.50"
	ts.SetInternalIP(lanIP)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	if _, err := c.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if err := c.InstallPackageInNetwork(ctx, "webpkg", "1.0", packages.Responses{"httpport": "8080"}, "fart"); err != nil {
		t.Fatalf("InstallPackageInNetwork: %v", err)
	}

	const wantFQDN = "webpkg.local.fart"
	const globalFQDN = "webpkg.local.home"

	// --- The ingress must serve the package under its NETWORK TLD ---
	if r := routeFor(ingMock.Routes, wantFQDN); r == nil {
		t.Fatalf("ingress has no route for %s; routes: %v", wantFQDN, routeHostnames(ingMock.Routes))
	}
	if r := routeFor(ingMock.Routes, globalFQDN); r != nil {
		t.Fatalf("ingress must NOT serve a network package under the global dns_tld (%s); routes: %v",
			globalFQDN, routeHostnames(ingMock.Routes))
	}

	// --- The state file, the route, and the cert must all name the SAME host ---
	st := readNetworkState(t, stateDir, "local", "webpkg", "1.0")
	if st.FQDN != wantFQDN {
		t.Fatalf("network state FQDN = %q, want %q", st.FQDN, wantFQDN)
	}
	sans := leafDNSNames(t, btrfsBase, "local", "webpkg", "1.0")
	if !slices.Contains(sans, wantFQDN) {
		t.Fatalf("leaf SANs %v missing %s — the ingress would serve a cert that is not valid for its own vhost", sans, wantFQDN)
	}
	if slices.Contains(sans, globalFQDN) {
		t.Fatalf("leaf SANs %v must not carry the global-TLD name %s", sans, globalFQDN)
	}

	// The route's cert dir must be the package's leaf (not ACME).
	route := routeFor(ingMock.Routes, wantFQDN)
	if route.GetAcme() || route.GetCertDir() == "" {
		t.Fatalf("network package must be served with its local-CA leaf, got acme=%v certdir=%q",
			route.GetAcme(), route.GetCertDir())
	}
	if route.GetBackend() == "" {
		t.Fatalf("route %s has no backend", wantFQDN)
	}
}

// TestIntegrationReconcilePreservesNetworkIngressRouteAndCert is the boot
// regression guard. Reconcile runs on every boot and used to recompute the TLD
// from the global dns_tld setting, so it re-issued the leaf with SAN
// webpkg.local.home — clobbering the correct .fart SAN written at install — and
// re-programmed the ingress with a .home vhost. The box therefore worked (at
// best) until the first restart. After the fix, a reconcile must leave the FQDN,
// the leaf SAN, and the ingress route all on the network TLD.
func TestIntegrationReconcilePreservesNetworkIngressRouteAndCert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	btrfsBase := t.TempDir()
	stateDir := t.TempDir()

	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0o600); err != nil {
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
description: "reconcile network TLD test"
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
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0o600); err != nil {
		t.Fatalf("WriteFile pkg: %v", err)
	}

	nm := initNetworkDB(t)
	inst := packages.NewInstallManager(dir)
	ingMock := &ingress.MockClient{}
	rolMock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:          mock,
		RepositoryRoot:   rr,
		Installer:        inst,
		Systemd:          sd,
		RolodexClient:    rolMock,
		IngressClient:    ingMock,
		SettingsMgr:      settings,
		NetworkMgr:       nm,
		BtrfsBasePath:    btrfsBase,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
	})
	t.Cleanup(func() { ts.Server.Close() })
	const lanIP = "192.168.122.50"
	ts.SetInternalIP(lanIP)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if _, err := c.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if err := c.InstallPackageInNetwork(ctx, "webpkg", "1.0", packages.Responses{"httpport": "8080"}, "fart"); err != nil {
		t.Fatalf("InstallPackageInNetwork: %v", err)
	}

	const wantFQDN = "webpkg.local.fart"
	const globalFQDN = "webpkg.local.home"

	// Simulate a boot: run the real Reconcile with the network manager and the
	// ingress client wired, exactly as main.go does.
	if err := systemcontroller.Reconcile(ctx, systemcontroller.ReconcileConfig{
		Installer:        inst,
		RepositoryRoot:   rr,
		Storage:          mock,
		Systemd:          sd,
		SettingsMgr:      settings,
		NetworkMgr:       nm,
		IngressClient:    ingMock,
		BtrfsBasePath:    btrfsBase,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
		InternalIP:       lanIP,
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// The state file must still name the package under its network TLD.
	st := readNetworkState(t, stateDir, "local", "webpkg", "1.0")
	if st.FQDN != wantFQDN {
		t.Fatalf("after reconcile: state FQDN = %q, want %q (reconcile recomputed the global dns_tld)", st.FQDN, wantFQDN)
	}

	// The leaf must NOT have been re-issued under the global TLD.
	sans := leafDNSNames(t, btrfsBase, "local", "webpkg", "1.0")
	if !slices.Contains(sans, wantFQDN) {
		t.Fatalf("after reconcile: leaf SANs %v missing %s", sans, wantFQDN)
	}
	if slices.Contains(sans, globalFQDN) {
		t.Fatalf("after reconcile: leaf was re-issued under the global TLD; SANs %v contain %s", sans, globalFQDN)
	}

	// The ingress must still be programmed with the network-TLD vhost.
	if r := routeFor(ingMock.Routes, wantFQDN); r == nil {
		t.Fatalf("after reconcile: ingress has no route for %s; routes: %v", wantFQDN, routeHostnames(ingMock.Routes))
	}
	if r := routeFor(ingMock.Routes, globalFQDN); r != nil {
		t.Fatalf("after reconcile: ingress re-programmed the package under the global dns_tld (%s); routes: %v",
			globalFQDN, routeHostnames(ingMock.Routes))
	}
}
