// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
)

func TestIngressHostPorts(t *testing.T) {
	compiled := &packages.Package{
		Network: packages.PackageNetwork{
			External:      packages.PortMap{3000: 3000, 2222: 22},
			InternalNames: packages.PortNameMap{3000: "http"},
		},
	}

	// Without supplies:[http], nothing is fronted by the ingress.
	if got := ingressHostPorts(compiled, nil); got != nil {
		t.Fatalf("expected nil without supplies http, got %v", got)
	}

	got := ingressHostPorts(compiled, []string{"http"})
	if !got[3000] {
		t.Fatalf("expected host port 3000 (named http) to be an ingress port, got %v", got)
	}
	if got[2222] {
		t.Fatalf("ssh port 2222 must not be an ingress port, got %v", got)
	}

	// A passthrough port owns its own TLS and is never an ingress port.
	compiled.Network.TLSModes = map[uint16]packages.TLSMode{3000: packages.TLSModePassthrough}
	if ingressHostPorts(compiled, []string{"http"})[3000] {
		t.Fatal("passthrough port must not be an ingress port")
	}
}

func TestGenerateIngressCaddyfilePagesAndPackages(t *testing.T) {
	content := GenerateIngressCaddyfile(
		[]PageCaddySite{{Name: "blog", Hostname: "blog.home", CertDir: "/etc/town-os/tls/leaves/pages/blog/current"}},
		[]PackageIngressSite{{Hostname: "gitea.default.home", CertDir: "/etc/town-os/tls/leaves/default/gitea/1.0", Backend: "town-os-package--default-gitea-1.0:3000"}},
	)

	// Page vhost: static file_server.
	for _, want := range []string{"https://blog.home {", "root * /srv/blog", "file_server"} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing page directive %q:\n%s", want, content)
		}
	}
	// Package vhost: reverse_proxy to the backend with the package leaf.
	for _, want := range []string{
		"https://gitea.default.home {",
		"tls /etc/town-os/tls/leaves/default/gitea/1.0/cert.pem /etc/town-os/tls/leaves/default/gitea/1.0/key.pem",
		"reverse_proxy town-os-package--default-gitea-1.0:3000",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing package directive %q:\n%s", want, content)
		}
	}
}

func TestGenerateIngressCaddyfileSkipsBackendlessAndUnprovisioned(t *testing.T) {
	content := GenerateIngressCaddyfile(nil, []PackageIngressSite{
		{Hostname: "no-backend.home", CertDir: "/leaf"},                 // no Backend -> skip
		{Hostname: "no-leaf.home", Backend: "c:80"},                     // no leaf, not ACME -> skip
		{Hostname: "ok.home", CertDir: "/leaf", Backend: "c:80"},        // kept
	})
	if strings.Contains(content, "no-backend.home") || strings.Contains(content, "no-leaf.home") {
		t.Fatalf("expected incomplete sites to be skipped:\n%s", content)
	}
	if !strings.Contains(content, "https://ok.home {") {
		t.Fatalf("expected complete site to render:\n%s", content)
	}
}

func TestCollectPackageIngressSites(t *testing.T) {
	dir := t.TempDir()

	// gitea: a TLS-terminated HTTP port (included) plus a raw SSH port (excluded).
	writeState(t, dir, "default", "gitea", "1.0", networkcontroller.PackageNetworkState{
		Repo: "default", Package: "gitea", Version: "1.0",
		ContainerName: "town-os-package--default-gitea-1.0",
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 443, InternalPort: 3000, TLS: true, Ingress: true, CertPath: "/etc/town-os/tls/leaves/default/gitea/1.0"},
			{ExternalPort: 2222, InternalPort: 22}, // raw SSH, not ingress -> excluded
		},
	})
	// sunshine: passthrough port -> excluded (owns its own TLS).
	writeState(t, dir, "default", "sunshine", "1.0", networkcontroller.PackageNetworkState{
		Repo: "default", Package: "sunshine", Version: "1.0",
		ContainerName: "town-os-package--default-sunshine-1.0",
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 47990, InternalPort: 47990, TLS: true, Passthrough: true, CertPath: "/x"},
		},
	})

	lister := &stubLister{items: []string{"default/gitea@1.0", "default/sunshine@1.0"}}
	sites := collectPackageIngressSites(lister, dir, "home")

	if len(sites) != 1 {
		t.Fatalf("expected exactly 1 ingress site (gitea http), got %d: %+v", len(sites), sites)
	}
	got := sites[0]
	if got.Hostname != "gitea.default.home" {
		t.Errorf("hostname = %q, want gitea.default.home", got.Hostname)
	}
	if got.Backend != "town-os-package--default-gitea-1.0:3000" {
		t.Errorf("backend = %q, want town-os-package--default-gitea-1.0:3000", got.Backend)
	}
	if got.CertDir != "/etc/town-os/tls/leaves/default/gitea/1.0" {
		t.Errorf("certdir = %q", got.CertDir)
	}
}

func TestCollectPackageIngressSitesNilLister(t *testing.T) {
	if got := collectPackageIngressSites(nil, "/x", "home"); got != nil {
		t.Fatalf("expected nil for nil lister, got %v", got)
	}
}

// writeState marshals a package network state to the state dir under the
// systemcontroller's <repo>-<pkg>-<ver>.json convention.
func writeState(t *testing.T, dir, repo, pkg, version string, st networkcontroller.PackageNetworkState) {
	t.Helper()
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	path := filepath.Join(dir, repo+"-"+pkg+"-"+version+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write state %s: %v", path, err)
	}
}
