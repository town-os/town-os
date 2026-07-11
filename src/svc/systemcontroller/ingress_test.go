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

func TestGenerateIngressCaddyfileHTTPSBackend(t *testing.T) {
	content := GenerateIngressCaddyfile(nil, []PackageIngressSite{
		{Hostname: "sunshine.default.home", CertDir: "/leaf", Backend: "c:47990", BackendTLS: true},
	})
	if !strings.Contains(content, "reverse_proxy https://c:47990 {") {
		t.Fatalf("expected https backend reverse_proxy:\n%s", content)
	}
	if !strings.Contains(content, "tls_insecure_skip_verify") {
		t.Fatalf("expected internal-hop verification skipped:\n%s", content)
	}
}

func TestIngressHostPortsHTTPSNamed(t *testing.T) {
	compiled := &packages.Package{
		Network: packages.PackageNetwork{
			External:      packages.PortMap{47990: 47990},
			InternalNames: packages.PortNameMap{47990: "https"},
		},
	}
	if !ingressHostPorts(compiled, []string{"http"})[47990] {
		t.Fatal("a port named \"https\" must be an ingress port")
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

func TestCollectPackageIngressSitesPublicDomain(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, "default", "gitea", "1.0", networkcontroller.PackageNetworkState{
		Repo: "default", Package: "gitea", Version: "1.0",
		ContainerName: "town-os-package--default-gitea-1.0",
		Ports: []networkcontroller.PortConfig{
			{
				ExternalPort: 3000, InternalPort: 3000, TLS: true, Ingress: true,
				CertPath:     "/etc/town-os/tls/leaves/default/gitea/1.0",
				PublicDomain: true, SNINames: []string{"git.example.com"},
			},
		},
	})

	sites := collectPackageIngressSites(&stubLister{items: []string{"default/gitea@1.0"}}, dir, "home")
	if len(sites) != 2 {
		t.Fatalf("expected base + public-FQDN sites, got %d: %+v", len(sites), sites)
	}

	var base, pub *PackageIngressSite
	for i := range sites {
		switch sites[i].Hostname {
		case "gitea.default.home":
			base = &sites[i]
		case "git.example.com":
			pub = &sites[i]
		}
	}
	if base == nil || base.ACME || base.CertDir == "" {
		t.Fatalf("base FQDN must be served with the local-CA leaf (no ACME): %+v", base)
	}
	if pub == nil || !pub.ACME {
		t.Fatalf("public FQDN must be served via ACME: %+v", pub)
	}
	if base.Backend != pub.Backend {
		t.Fatalf("both vhosts must proxy the same backend: %q vs %q", base.Backend, pub.Backend)
	}
}

func TestCollectPackageIngressSitesNilLister(t *testing.T) {
	if got := collectPackageIngressSites(nil, "/x", "home"); got != nil {
		t.Fatalf("expected nil for nil lister, got %v", got)
	}
}

// A package installed into a non-default WireGuard network is served under THAT
// network's TLD, taken from the state file's FQDN — never under the global
// dns_tld. This is the bug that made gitea.default.fart resolve on the LAN but
// never get served: the ingress rendered a gitea.default.home vhost that nothing
// dialed, and the leaf it attached was valid only for .fart.
func TestCollectPackageIngressSitesUsesStateFQDN(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, "local", "gitea", "1.0", networkcontroller.PackageNetworkState{
		Repo: "local", Package: "gitea", Version: "1.0",
		ContainerName: "town-os-package--local-gitea-1.0",
		FQDN:          "gitea.local.fart",
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 443, InternalPort: 3000, TLS: true, Ingress: true, CertPath: "/etc/town-os/tls/leaves/local/gitea/1.0"},
		},
	})

	// The global dns_tld is "home" — it must NOT win over the state file.
	sites := collectPackageIngressSites(&stubLister{items: []string{"local/gitea@1.0"}}, dir, "home")
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(sites), sites)
	}
	if sites[0].Hostname != "gitea.local.fart" {
		t.Fatalf("hostname = %q, want gitea.local.fart (the network TLD, not the global dns_tld)", sites[0].Hostname)
	}
}

// State files written before the FQDN field existed carry no fqdn key. They must
// keep resolving under the global dns_tld (today's behavior for default-network
// packages) and self-heal on the next reconcile, which rewrites them.
func TestCollectPackageIngressSitesFallsBackWhenFQDNEmpty(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, "default", "gitea", "1.0", networkcontroller.PackageNetworkState{
		Repo: "default", Package: "gitea", Version: "1.0",
		ContainerName: "town-os-package--default-gitea-1.0",
		// no FQDN — pre-upgrade state file
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 443, InternalPort: 3000, TLS: true, Ingress: true, CertPath: "/x"},
		},
	})

	sites := collectPackageIngressSites(&stubLister{items: []string{"default/gitea@1.0"}}, dir, "home")
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	if sites[0].Hostname != "gitea.default.home" {
		t.Fatalf("hostname = %q, want the global-TLD fallback gitea.default.home", sites[0].Hostname)
	}
}

// The public-FQDN dedupe compares against the network-correct FQDN, so a dep
// declaring its own internal name as an SNI name does not produce a duplicate
// vhost (which would make caddy reject the whole config).
func TestCollectPackageIngressSitesNetworkFQDNDedupesSNI(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, "default", "gitea", "2.0", networkcontroller.PackageNetworkState{
		Repo: "default", Package: "gitea", Version: "2.0",
		ContainerName: "town-os-package--default-gitea-2.0",
		FQDN:          "gitea.default.fart",
		Ports: []networkcontroller.PortConfig{
			{
				ExternalPort: 443, InternalPort: 3000, TLS: true, Ingress: true,
				CertPath:     "/x",
				PublicDomain: true,
				SNINames:     []string{"gitea.default.fart", "git.example.com"},
			},
		},
	})

	sites := collectPackageIngressSites(&stubLister{items: []string{"default/gitea@2.0"}}, dir, "home")
	if len(sites) != 2 {
		t.Fatalf("expected exactly 2 sites (internal + public), got %d: %+v", len(sites), sites)
	}
	seen := map[string]bool{}
	for _, s := range sites {
		if seen[s.Hostname] {
			t.Fatalf("duplicate vhost %q would make caddy reject the config", s.Hostname)
		}
		seen[s.Hostname] = true
	}
	if !seen["gitea.default.fart"] || !seen["git.example.com"] {
		t.Fatalf("want gitea.default.fart + git.example.com, got %+v", seen)
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
