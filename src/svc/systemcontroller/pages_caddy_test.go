// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

func TestGeneratePagesCaddyfileInternal(t *testing.T) {
	content := GeneratePagesCaddyfile([]PageCaddySite{
		{Name: "blog", Hostname: "blog.home", CertDir: "/etc/town-os/tls/leaves/pages/blog/current"},
	})
	for _, want := range []string{
		"auto_https off",
		"https://blog.home {",
		"tls /etc/town-os/tls/leaves/pages/blog/current/cert.pem /etc/town-os/tls/leaves/pages/blog/current/key.pem",
		"root * /srv/blog",
		"file_server",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected Caddyfile to contain %q, got:\n%s", want, content)
		}
	}
	// Internal sites must never reach for ACME.
	if strings.Contains(content, "issuer acme") {
		t.Fatalf("internal site should not use ACME, got:\n%s", content)
	}
}

func TestGeneratePagesCaddyfilePublicUsesACME(t *testing.T) {
	content := GeneratePagesCaddyfile([]PageCaddySite{
		{Name: "site", Hostname: "site.example.com", ACME: true},
	})
	if !strings.Contains(content, "https://site.example.com {") {
		t.Fatalf("expected public vhost, got:\n%s", content)
	}
	if !strings.Contains(content, "issuer acme") {
		t.Fatalf("expected ACME issuer for public FQDN, got:\n%s", content)
	}
	if strings.Contains(content, "root * /srv/site") == false {
		t.Fatalf("expected webroot for public site, got:\n%s", content)
	}
}

func TestGeneratePagesCaddyfileSkipsUnprovisioned(t *testing.T) {
	// An internal site with no issued leaf yet must be skipped rather than
	// emit a tls directive with no cert path (which breaks the whole config).
	content := GeneratePagesCaddyfile([]PageCaddySite{
		{Name: "blog", Hostname: "blog.home", CertDir: ""},
	})
	if strings.Contains(content, "https://blog.home") {
		t.Fatalf("expected unprovisioned site to be skipped, got:\n%s", content)
	}
}

func TestGeneratePagesUnit(t *testing.T) {
	unit := GeneratePagesUnit(PagesUnitConfig{
		BtrfsBasePath: "/data",
		CaddyImage:    "docker.io/library/caddy:latest",
	})

	if unit.Name != PagesUnitName {
		t.Fatalf("expected unit name %q, got %q", PagesUnitName, unit.Name)
	}

	if !strings.Contains(unit.Content, PagesContainerName) {
		t.Fatal("expected unit content to contain container name")
	}

	if !strings.Contains(unit.Content, "--net "+systemd.IngressNetworkName) {
		t.Fatalf("expected ingress network %q, got:\n%s", systemd.IngressNetworkName, unit.Content)
	}
	if !strings.Contains(unit.Content, "-p 443:443") {
		t.Fatalf("expected the ingress to publish :443, got:\n%s", unit.Content)
	}
	if !strings.Contains(unit.Content, "podman network create "+systemd.IngressNetworkName) {
		t.Fatalf("expected the ingress network to be created, got:\n%s", unit.Content)
	}

	if !strings.Contains(unit.Content, "/data/pages:/data/pages:ro,z") {
		t.Fatal("expected unit content to contain pages volume mount")
	}

	if !strings.Contains(unit.Content, "/data/pages-webroot:/srv:ro,z") {
		t.Fatal("expected unit content to contain webroot volume mount")
	}

	if !strings.Contains(unit.Content, "/data/pages-caddy/Caddyfile:/etc/caddy/Caddyfile:ro,z") {
		t.Fatal("expected unit content to contain Caddyfile volume mount")
	}

	// The TLS subvolume must be mounted so Caddy can read per-page leaf certs.
	if !strings.Contains(unit.Content, "/data/tls:/etc/town-os/tls:ro,z") {
		t.Fatalf("expected unit content to contain TLS volume mount, got:\n%s", unit.Content)
	}

	if !strings.Contains(unit.Content, "Restart=on-failure") {
		t.Fatal("expected unit content to contain Restart=on-failure")
	}

	// Restart= without RestartSec= inherits systemd's 100ms default and turns a
	// unit that cannot start into a retry storm. The systemd package sweeps its
	// own generators for this (TestEveryGeneratedUnitSetsRestartSec); this one
	// lives out here, so it needs its own assertion.
	if !strings.Contains(unit.Content, fmt.Sprintf("RestartSec=%d", systemd.RestartSecDefault)) {
		t.Fatalf("expected unit content to contain RestartSec, got:\n%s", unit.Content)
	}

	if !strings.Contains(unit.Content, "docker.io/library/caddy:latest") {
		t.Fatal("expected unit content to contain caddy image")
	}
}

func TestEnsurePageSymlink(t *testing.T) {
	dir := t.TempDir()

	// Create the webroot directory.
	if err := EnsurePagesWebroot(dir); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	if err := EnsurePageSymlink(dir, "my-site"); err != nil {
		t.Fatalf("EnsurePageSymlink: %v", err)
	}

	linkPath := filepath.Join(dir, PagesWebrootDir, "my-site")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}

	expected := "/data/pages/my-site"
	if target != expected {
		t.Fatalf("expected symlink target %q, got %q", expected, target)
	}
}

func TestEnsurePageSymlinkIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := EnsurePagesWebroot(dir); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	// Create twice — should not error.
	if err := EnsurePageSymlink(dir, "my-site"); err != nil {
		t.Fatalf("first EnsurePageSymlink: %v", err)
	}
	if err := EnsurePageSymlink(dir, "my-site"); err != nil {
		t.Fatalf("second EnsurePageSymlink: %v", err)
	}

	linkPath := filepath.Join(dir, PagesWebrootDir, "my-site")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}

	expected := "/data/pages/my-site"
	if target != expected {
		t.Fatalf("expected symlink target %q, got %q", expected, target)
	}
}

func TestRemovePageSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := EnsurePagesWebroot(dir); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}
	if err := EnsurePageSymlink(dir, "my-site"); err != nil {
		t.Fatalf("EnsurePageSymlink: %v", err)
	}

	if err := RemovePageSymlink(dir, "my-site"); err != nil {
		t.Fatalf("RemovePageSymlink: %v", err)
	}

	linkPath := filepath.Join(dir, PagesWebrootDir, "my-site")
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Fatal("expected symlink to be removed")
	}
}

func TestRemovePageSymlinkNonexistent(t *testing.T) {
	dir := t.TempDir()
	if err := EnsurePagesWebroot(dir); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	// Should not error for non-existent symlink.
	if err := RemovePageSymlink(dir, "nonexistent"); err != nil {
		t.Fatalf("RemovePageSymlink: %v", err)
	}
}

func TestWritePagesCaddyfile(t *testing.T) {
	dir := t.TempDir()
	sites := []PageCaddySite{{Name: "blog", Hostname: "blog.home", CertDir: "/etc/town-os/tls/leaves/pages/blog/current"}}

	path, changed, err := WritePagesCaddyfile(dir, sites)
	if err != nil {
		t.Fatalf("WritePagesCaddyfile: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first write")
	}

	expectedPath := filepath.Join(dir, PagesCaddyDir, "Caddyfile")
	if path != expectedPath {
		t.Fatalf("expected path %q, got %q", expectedPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "https://blog.home {") {
		t.Fatalf("expected Caddyfile to contain the vhost, got:\n%s", string(data))
	}

	// Re-writing identical content reports no change so callers don't bounce
	// the running Caddy needlessly.
	if _, changed, err = WritePagesCaddyfile(dir, sites); err != nil {
		t.Fatalf("WritePagesCaddyfile (rewrite): %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when content is identical")
	}
}

func TestEnsurePagesWebroot(t *testing.T) {
	dir := t.TempDir()

	if err := EnsurePagesWebroot(dir); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}

	webrootPath := filepath.Join(dir, PagesWebrootDir)
	info, err := os.Stat(webrootPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected pages-webroot to be a directory")
	}
}

func TestCaddyImageDefault(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{}}
	got := s.caddyImage(t.Context())
	if got != DefaultCaddyImage {
		t.Fatalf("expected %q, got %q", DefaultCaddyImage, got)
	}
}

func TestCaddyImageFromSettings(t *testing.T) {
	mgr := &testSettingsManager{values: map[string]string{
		"caddy_image": "docker.io/library/caddy:2.7",
	}}
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{settingsMgr: mgr}}
	got := s.caddyImage(t.Context())
	if got != "docker.io/library/caddy:2.7" {
		t.Fatalf("expected %q, got %q", "docker.io/library/caddy:2.7", got)
	}
}

