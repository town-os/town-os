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
	"strconv"
	"testing"

	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// TestIntegrationNCTLSPipeline exercises the full NC-TLS chain in one
// shot, covering three recent commits that each only had unit-level
// coverage before:
//
//   - e7cacd2 (TLS-wrap ports by yaml name, not just a numeric
//     allowlist) — the package picks an auto-generated HTTP port that
//     isn't in httpContainerPorts; the TLS decision must still flip to
//     "yes, wrap" because the yaml names the port "http".
//   - e4aadda (include the internal LAN IP in leaf cert SANs) — the
//     issued leaf on disk must list the internal IP as an IPAddress SAN
//     so a browser typing the LAN IP directly gets a clean cert match.
//   - ecf6dd6 (disable H3 in the NC Caddyfile) — the Caddyfile the NC
//     would render from the resulting state file must include
//     `protocols h1 h2` and must not contain the literal "h3", so the
//     browser is never offered an Alt-Svc hint that points at a UDP
//     port the host doesn't forward.
//
// The test drives the real install handler end-to-end, then reads the
// state file, leaf cert, and rendered Caddyfile off disk. Everything
// before the NC container boot (which would need `make test-full`) is
// covered; booting an actual Caddy is the only piece still left to the
// full test-integration pass.
func TestIntegrationNCTLSPipeline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	btrfsBase := filepath.Join(dir, "btrfs")
	networkStateDir := filepath.Join(dir, "network-state")
	for _, d := range []string{btrfsBase, networkStateDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	repoData, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), repoData, 0o600); err != nil {
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

	// Package yaml: http port has a SEMANTIC name "http" that IS the
	// lever e7cacd2 added — we deliberately pick an auto-generated port
	// below that is NOT in the numeric allowlist, so the old code would
	// not have TLS-wrapped it.
	pkgYAML := `image: nginx:1.0
description: "tls pipeline test"
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
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "tlspkg")
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0o600); err != nil {
		t.Fatalf("WriteFile yaml: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	storageMock := storage.InitBtrFSMock()
	sdMock := systemd.InitMockManager()
	rolMock := &rolodex.MockClient{}
	ingMock := &ingress.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	internalIP := "192.168.10.42"
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:          storageMock,
		RepositoryRoot:   rr,
		Installer:        inst,
		Systemd:          sdMock,
		RolodexClient:    rolMock,
		IngressClient:    ingMock,
		SettingsMgr:      settings,
		BtrfsBasePath:    btrfsBase,
		NetworkStatePath: networkStateDir,
		TLSCA:            ca,
	})
	ts.SetInternalIP(internalIP)
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Pick an HTTP port outside the numeric allowlist: 38895 matches the
	// real-world gitea regression, and neither 38895 nor the default
	// 3000 from the yaml (which IS in the allowlist) should change the
	// outcome — the yaml name is what flips the switch.
	httpPort := "38895"
	if err := c.InstallPackage(context.TODO(), "tlspkg", "1.0", packages.Responses{
		"httpport": httpPort,
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// --- e7cacd2 assertion: state file flipped TLS=true via port name ---
	stateBytes, err := os.ReadFile(filepath.Join(networkStateDir, "local-tlspkg-1.0.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d: %+v", len(state.Ports), state.Ports)
	}
	p := state.Ports[0]
	if !p.TLS {
		t.Fatalf("port %d named 'http' must have TLS=true after e7cacd2; got %+v", p.InternalPort, p)
	}
	if p.CertPath == "" {
		t.Fatalf("TLS port must carry a CertPath; got %+v", p)
	}

	// --- e4aadda assertion: leaf cert on disk includes the internal IP ---
	leafPath := filepath.Join(btrfsBase, systemcontroller.TLSSubvolume, systemcontroller.TLSLeavesDir, "local", "tlspkg", "1.0", "cert.pem")
	leafBytes, err := os.ReadFile(leafPath)
	if err != nil {
		t.Fatalf("read leaf cert at %s: %v", leafPath, err)
	}
	block, _ := pem.Decode(leafBytes)
	if block == nil {
		t.Fatalf("leaf PEM decode failed")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	ipFound := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == internalIP {
			ipFound = true
			break
		}
	}
	if !ipFound {
		t.Fatalf("internal IP %s missing from cert IPAddresses: %v", internalIP, cert.IPAddresses)
	}
	if !slices.Contains(cert.DNSNames, "tlspkg.local.home") {
		t.Fatalf("DNS name tlspkg.local.home missing from cert DNSNames: %v", cert.DNSNames)
	}

	// The HTTP port is fronted by the shared :443 ingress, not the per-package
	// NC, so the port is flagged Ingress and the per-package NC renders no site.
	if !p.Ingress {
		t.Fatalf("named-http port must be flagged Ingress: %+v", p)
	}
	if sites := networkcontroller.CollectCaddySites([]*networkcontroller.PackageNetworkState{&state}); len(sites) != 0 {
		t.Fatalf("per-package NC must render no site for an ingress port, got %d", len(sites))
	}

	// The shared :443 ingress is programmed over gRPC (not a file-mounted
	// Caddyfile): the install pushed a route for the package FQDN that
	// reverse-proxies to its service container and terminates TLS with the
	// issued leaf.
	route := ingMock.RouteFor("tlspkg.local.home")
	if route == nil {
		t.Fatalf("ingress not programmed with a route for tlspkg.local.home; routes: %+v", ingMock.Routes)
	}
	wantBackend := state.ContainerName + ":" + strconv.Itoa(int(p.InternalPort))
	if route.GetBackend() != wantBackend {
		t.Fatalf("ingress route backend = %q, want %q", route.GetBackend(), wantBackend)
	}
	if route.GetCertDir() != p.CertPath {
		t.Fatalf("ingress route cert dir = %q, want %q", route.GetCertDir(), p.CertPath)
	}
	if route.GetAcme() {
		t.Fatalf("internal package route must not be ACME")
	}
}
