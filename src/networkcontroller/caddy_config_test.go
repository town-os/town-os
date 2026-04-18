// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectCaddySitesSortsAndFiltersNonTLS(t *testing.T) {
	tmp := t.TempDir()
	certDirA := filepath.Join(tmp, "a")
	certDirB := filepath.Join(tmp, "b")
	if err := os.MkdirAll(certDirA, 0o750); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.MkdirAll(certDirB, 0o750); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(certDirA, "cert.pem"), []byte("cert-a"), 0o600); err != nil {
		t.Fatalf("write cert a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(certDirB, "cert.pem"), []byte("cert-b"), 0o600); err != nil {
		t.Fatalf("write cert b: %v", err)
	}

	plex := &PackageNetworkState{
		Repo:          "default",
		Package:       "plex",
		Version:       "1.0",
		ContainerName: "town-os-default-plex-1.0",
		Ports: []PortConfig{
			{ExternalPort: 21305, InternalPort: 32400, Forward: true, TLS: true, CertPath: certDirA},
		},
	}
	gitea := &PackageNetworkState{
		Repo:          "default",
		Package:       "gitea",
		Version:       "1.0",
		ContainerName: "town-os-default-gitea-1.0",
		Ports: []PortConfig{
			{ExternalPort: 12000, InternalPort: 3000, Forward: true, TLS: true, CertPath: certDirB},
			// SSH is forwarded but not TLS — should be ignored by the Caddy collector.
			{ExternalPort: 12001, InternalPort: 22, Forward: true, TLS: false},
		},
	}
	// A package whose port isn't forwarded at all — totally excluded.
	inert := &PackageNetworkState{
		ContainerName: "inert",
		Ports: []PortConfig{
			{ExternalPort: 99, InternalPort: 99, Forward: false, TLS: true, CertPath: certDirA},
		},
	}

	sites := CollectCaddySites([]*PackageNetworkState{plex, gitea, inert})
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d: %+v", len(sites), sites)
	}
	// Sorted by external port ascending: gitea (12000) before plex (21305).
	if sites[0].ExternalPort != 12000 || sites[1].ExternalPort != 21305 {
		t.Fatalf("sites not sorted by external port: %+v", sites)
	}
	if sites[0].Target != "town-os-default-gitea-1.0" || sites[1].Target != "town-os-default-plex-1.0" {
		t.Fatalf("wrong targets: %+v", sites)
	}

	wantHashB := sha256Hex("cert-b")
	wantHashA := sha256Hex("cert-a")
	if sites[0].CertHash != wantHashB {
		t.Fatalf("gitea cert hash mismatch: got %q want %q", sites[0].CertHash, wantHashB)
	}
	if sites[1].CertHash != wantHashA {
		t.Fatalf("plex cert hash mismatch: got %q want %q", sites[1].CertHash, wantHashA)
	}
}

func TestRenderCaddyfileHasOneSitePerPort(t *testing.T) {
	sites := []CaddySite{
		{ExternalPort: 12000, Target: "gitea", InternalPort: 3000, CertPath: "/etc/town-os/tls/leaves/default/gitea/1.0", CertHash: "deadbeef"},
		{ExternalPort: 21305, Target: "plex", InternalPort: 32400, CertPath: "/etc/town-os/tls/leaves/default/plex/1.0", CertHash: "cafef00d"},
	}
	out := string(RenderCaddyfile(sites))

	// Global stanza.
	if !strings.Contains(out, "auto_https off") {
		t.Fatalf("missing auto_https off:\n%s", out)
	}
	if !strings.Contains(out, "admin off") {
		t.Fatalf("missing admin off:\n%s", out)
	}

	// Exactly two site blocks.
	if got := strings.Count(out, "reverse_proxy "); got != 2 {
		t.Fatalf("expected 2 reverse_proxy lines, got %d:\n%s", got, out)
	}

	for _, want := range []string{
		"https://:12000 {",
		"# cert-hash: deadbeef",
		"tls /etc/town-os/tls/leaves/default/gitea/1.0/cert.pem /etc/town-os/tls/leaves/default/gitea/1.0/key.pem",
		"reverse_proxy gitea:3000",
		"https://:21305 {",
		"# cert-hash: cafef00d",
		"tls /etc/town-os/tls/leaves/default/plex/1.0/cert.pem /etc/town-os/tls/leaves/default/plex/1.0/key.pem",
		"reverse_proxy plex:32400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in rendered Caddyfile:\n%s", want, out)
		}
	}
}

// TestRenderCaddyfileDisablesH3 pins the fix for the "browser hangs on
// gitea URL" report: Caddy's default config advertises
// `Alt-Svc: h3=":<port>"` on every response and tries to listen on UDP
// <port>, but podman's `-p NNN:NNN` only forwards TCP — the UDP
// listener lives inside the container netns and is unreachable from
// the host. Browsers cache the Alt-Svc header, switch to H3 on the
// next request, and hang waiting for UDP that will never arrive. The
// `protocols h1 h2` line in the global servers block both turns off
// the UDP listener (nothing to wait for) and drops the Alt-Svc header
// (nothing to cache).
func TestRenderCaddyfileDisablesH3(t *testing.T) {
	sites := []CaddySite{
		{ExternalPort: 12000, Target: "gitea", InternalPort: 3000, CertPath: "/x"},
	}
	out := string(RenderCaddyfile(sites))
	if !strings.Contains(out, "protocols h1 h2") {
		t.Fatalf("expected `protocols h1 h2` in global servers block:\n%s", out)
	}
	if strings.Contains(out, "h3") {
		t.Fatalf("rendered Caddyfile must not mention h3:\n%s", out)
	}
}

func TestRenderCaddyfileSkipsMissingCertPath(t *testing.T) {
	sites := []CaddySite{
		{ExternalPort: 12000, Target: "gitea", InternalPort: 3000, CertPath: ""},
	}
	out := string(RenderCaddyfile(sites))
	if strings.Contains(out, "https://:12000") {
		t.Fatalf("expected site with empty CertPath to be skipped:\n%s", out)
	}
}

func TestRenderCaddyfileChangesWhenCertHashChanges(t *testing.T) {
	sites := []CaddySite{
		{ExternalPort: 12000, Target: "gitea", InternalPort: 3000, CertPath: "/tls/gitea", CertHash: "aaa"},
	}
	a := RenderCaddyfile(sites)
	sites[0].CertHash = "bbb"
	b := RenderCaddyfile(sites)
	if string(a) == string(b) {
		t.Fatal("expected Caddyfile bytes to change when cert hash changes, but they were identical")
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
