package systemcontroller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCaddyfile(t *testing.T) {
	content := GenerateCaddyfile("8080")
	if !strings.Contains(content, ":8080") {
		t.Fatalf("expected Caddyfile to contain :8080, got:\n%s", content)
	}
	if !strings.Contains(content, "root * /srv") {
		t.Fatal("expected Caddyfile to contain root directive")
	}
	if !strings.Contains(content, "file_server") {
		t.Fatal("expected Caddyfile to contain file_server directive")
	}
	if !strings.Contains(content, "respond / 404") {
		t.Fatal("expected Caddyfile to contain respond / 404")
	}
}

func TestGenerateCaddyfileCustomPort(t *testing.T) {
	content := GenerateCaddyfile("9090")
	if !strings.Contains(content, ":9090") {
		t.Fatalf("expected Caddyfile to contain :9090, got:\n%s", content)
	}
}

func TestGeneratePagesUnit(t *testing.T) {
	unit := GeneratePagesUnit(PagesUnitConfig{
		BtrfsBasePath: "/data",
		CaddyImage:    "docker.io/library/caddy:latest",
		CaddyPort:     "8080",
	})

	if unit.Name != PagesUnitName {
		t.Fatalf("expected unit name %q, got %q", PagesUnitName, unit.Name)
	}

	if !strings.Contains(unit.Content, PagesContainerName) {
		t.Fatal("expected unit content to contain container name")
	}

	if !strings.Contains(unit.Content, "--net host") {
		t.Fatal("expected unit content to contain --net host")
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

	if !strings.Contains(unit.Content, "Restart=on-failure") {
		t.Fatal("expected unit content to contain Restart=on-failure")
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

func TestWriteCaddyfile(t *testing.T) {
	dir := t.TempDir()

	path, err := WriteCaddyfile(dir, "8080")
	if err != nil {
		t.Fatalf("WriteCaddyfile: %v", err)
	}

	expectedPath := filepath.Join(dir, PagesCaddyDir, "Caddyfile")
	if path != expectedPath {
		t.Fatalf("expected path %q, got %q", expectedPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ":8080") {
		t.Fatalf("expected Caddyfile to contain :8080, got:\n%s", content)
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
	got := s.caddyImage()
	if got != DefaultCaddyImage {
		t.Fatalf("expected %q, got %q", DefaultCaddyImage, got)
	}
}

func TestCaddyImageFromSettings(t *testing.T) {
	mgr := &testSettingsManager{values: map[string]string{
		"caddy_image": "docker.io/library/caddy:2.7",
	}}
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{settingsMgr: mgr}}
	got := s.caddyImage()
	if got != "docker.io/library/caddy:2.7" {
		t.Fatalf("expected %q, got %q", "docker.io/library/caddy:2.7", got)
	}
}

func TestCaddyPortDefault(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{}}
	got := s.caddyPort()
	if got != DefaultCaddyPort {
		t.Fatalf("expected %q, got %q", DefaultCaddyPort, got)
	}
}

func TestCaddyPortFromSettings(t *testing.T) {
	mgr := &testSettingsManager{values: map[string]string{
		"caddy_port": "9090",
	}}
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{settingsMgr: mgr}}
	got := s.caddyPort()
	if got != "9090" {
		t.Fatalf("expected %q, got %q", "9090", got)
	}
}
