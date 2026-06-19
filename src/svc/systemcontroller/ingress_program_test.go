// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	if err := os.WriteFile(filepath.Join(stateDir, "asdf-gitea-1.0.json"), data, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	lister := &stubLister{items: []string{"asdf/gitea@1.0"}}
	routes := buildIngressRoutes(nil, lister, nil, "", stateDir, "home", "")

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
	if err := os.WriteFile(filepath.Join(stateDir, "asdf-gitea-1.0.json"), data, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	ic := &mockIngressClient{}
	lister := &stubLister{items: []string{"asdf/gitea@1.0"}}
	if err := RebuildIngress(context.Background(), ic, nil, lister, nil, "", stateDir, "home", ""); err != nil {
		t.Fatalf("RebuildIngress: %v", err)
	}
	if len(ic.setCalls) != 1 {
		t.Fatalf("expected 1 SetRoutes call, got %d", len(ic.setCalls))
	}
	if len(ic.setCalls[0]) != 1 || ic.setCalls[0][0].GetHostname() != "gitea.asdf.home" {
		t.Fatalf("unexpected routes pushed: %+v", ic.setCalls[0])
	}
}
