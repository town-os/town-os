// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/networkcontroller"
)

// TestBuildIngressRoutesPackages verifies that buildIngressRoutes turns an
// installed HTTP package's network state into one ingress Route keyed by the
// package FQDN, proxying to the service container, and ignores raw (non-ingress)
// ports.
func TestBuildIngressRoutesPackages(t *testing.T) {
	stateDir := t.TempDir()
	st := networkcontroller.PackageNetworkState{
		Repo:          "asdf",
		Package:       "gitea",
		Version:       "1.0",
		ContainerName: "town-os-package--asdf-gitea-1.0",
		Ports: []networkcontroller.PortConfig{
			// HTTP ingress port → one route.
			{ExternalPort: 3000, InternalPort: 3000, TLS: true, Ingress: true, CertPath: "/etc/town-os/tls/leaves/asdf/gitea/1.0"},
			// Raw forwarded SSH port → not an ingress route.
			{ExternalPort: 10593, InternalPort: 22, Forward: true},
		},
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "asdf-gitea-1.0.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	lister := &stubLister{items: []string{"asdf/gitea@1.0"}}
	routes := buildIngressRoutes(context.Background(), nil, nil, lister, nil, nil, "", stateDir, "home", "")

	if len(routes) != 1 {
		t.Fatalf("expected exactly 1 package route, got %d: %+v", len(routes), routes)
	}
	r := routes[0]
	if r.GetHostname() != "gitea.asdf.home" {
		t.Errorf("hostname = %q, want gitea.asdf.home", r.GetHostname())
	}
	if r.GetBackend() != "town-os-package--asdf-gitea-1.0:3000" {
		t.Errorf("backend = %q, want town-os-package--asdf-gitea-1.0:3000", r.GetBackend())
	}
	if r.GetCertDir() != "/etc/town-os/tls/leaves/asdf/gitea/1.0" {
		t.Errorf("cert_dir = %q", r.GetCertDir())
	}
	if r.GetAcme() {
		t.Error("internal package route should not be ACME")
	}
}

// TestRebuildIngressPushesRoutes verifies RebuildIngress hands the collected
// routes to the ingress client via SetRoutes.
func TestRebuildIngressPushesRoutes(t *testing.T) {
	stateDir := t.TempDir()
	st := networkcontroller.PackageNetworkState{
		Repo: "asdf", Package: "gitea", Version: "1.0",
		ContainerName: "town-os-package--asdf-gitea-1.0",
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 3000, InternalPort: 3000, TLS: true, Ingress: true, CertPath: "/c"},
		},
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "asdf-gitea-1.0.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ic := &mockIngressClient{}
	lister := &stubLister{items: []string{"asdf/gitea@1.0"}}
	if err := RebuildIngress(context.Background(), ic, nil, nil, lister, nil, nil, "", stateDir, "home", ""); err != nil {
		t.Fatalf("RebuildIngress: %v", err)
	}
	if len(ic.setCalls) != 1 {
		t.Fatalf("expected 1 SetRoutes call, got %d", len(ic.setCalls))
	}
	if len(ic.setCalls[0]) != 1 || ic.setCalls[0][0].GetHostname() != "gitea.asdf.home" {
		t.Fatalf("unexpected routes pushed: %+v", ic.setCalls[0])
	}
}

// A page on a non-default network is served under THAT network's TLD, while a
// page on the default network keeps the global dns_tld — both in the same route
// set. This is the page-side twin of the package fix: naming the vhost from the
// global TLD would render a blog.home site that nothing dials, with a leaf valid
// only for blog.fart.
func TestBuildIngressRoutesPagesUseNetworkTLD(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{
		Name: "fart", TLD: "fart", Subnet: "10.65.0.0/24", Address: "10.65.0.1/24",
		PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("create network: %v", err)
	}

	pagesMgr := &pagesTestManager{pages: []account.PageSite{
		{Name: "blog"},                    // default network → blog.home
		{Name: "secret", Network: "fart"}, // fart network   → secret.fart
	}}

	// ca == nil, so no leaf is issued and CertDir stays empty; we are asserting
	// the hostnames the ingress vhosts are keyed by.
	routes := buildIngressRoutes(context.Background(), pagesMgr, nm, &stubLister{}, nil, nil, "", "", "home", "")

	hosts := map[string]bool{}
	for _, r := range routes {
		hosts[r.GetHostname()] = true
	}
	if !hosts["blog.home"] {
		t.Errorf("default-network page must be served at blog.home; got %v", hosts)
	}
	if !hosts["secret.fart"] {
		t.Errorf("fart-network page must be served at secret.fart; got %v", hosts)
	}
	if hosts["secret.home"] {
		t.Errorf("fart-network page must NOT be served under the global dns_tld; got %v", hosts)
	}
}
