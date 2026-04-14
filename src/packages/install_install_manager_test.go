// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- InstallManager response persistence tests ---

func TestInstallManagerResponsesPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create a fake repo package so Install succeeds.
	pkgDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	err := os.MkdirAll(pkgDir, 0750)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	err = os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)
	resp := Responses{"port": "8080", "hostname": "myhost"}

	err = mgr.Install("test-repo", "nginx", "nginx", "1.0", resp)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := mgr.GetResponses("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	if got["port"] != "8080" || got["hostname"] != "myhost" {
		t.Fatalf("expected port=8080 hostname=myhost, got %v", got)
	}
}

func TestInstallManagerDependencyUsesSourcePkgName(t *testing.T) {
	dir := t.TempDir()

	// Create a repo package under the real name "postgres".
	pkgDir := filepath.Join(dir, "default", PackagesDir, "postgres")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "15.0.yaml"), []byte("image: postgres:15\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)

	// Install with effective name "myapp--dep--db" but source name "postgres".
	if err := mgr.Install("default", "myapp--dep--db", "postgres", "15.0", Responses{}); err != nil {
		t.Fatalf("Install dependency: %v", err)
	}

	// The installed record should exist at the nested on-disk path
	// (parent/subpackages/<key>/<version>.yaml). The effective name in
	// memory is still the flat "myapp--dep--db" form — only disk storage
	// reflects the nested layout.
	link := filepath.Join(dir, InstalledDir, "default", "myapp", SubpackagesDir, "db", "15.0.yaml")
	if _, err := os.Stat(link); err != nil {
		t.Fatalf("installed record missing at nested path: %v", err)
	}
	// The old flat path must NOT exist — regression guard against any
	// future helper accidentally bypassing StoragePath.
	flat := filepath.Join(dir, InstalledDir, "default", "myapp--dep--db", "15.0.yaml")
	if _, err := os.Stat(flat); err == nil {
		t.Fatal("unexpected flat-layout install record — StoragePath translation missing")
	}

	// Using the effective name as source should fail (no such directory).
	if err := mgr.Install("default", "myapp--dep--cache", "myapp--dep--cache", "15.0", Responses{}); err == nil {
		t.Fatal("expected error when sourcePkgName does not exist in repo")
	}
}

func TestInstallManagerGetResponsesNotInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	_, err := mgr.GetResponses("test-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for not-installed package")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestInstallManagerUninstallRemovesResponses(t *testing.T) {
	dir := t.TempDir()

	// Create a fake repo package.
	pkgDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	err := os.MkdirAll(pkgDir, 0750)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	err = os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)

	err = mgr.Install("test-repo", "nginx", "nginx", "1.0", Responses{"port": "80"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	err = mgr.Uninstall("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	_, err = mgr.GetResponses("test-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error after uninstall")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}

	// Verify response directory was cleaned up.
	respDir := filepath.Join(dir, ResponsesDir, "test-repo", "nginx")
	_, err = os.Stat(respDir)
	if !os.IsNotExist(err) {
		t.Fatalf("expected response directory to be cleaned up, got err: %v", err)
	}
}

func TestGetInstalledVersionFound(t *testing.T) {
	dir := t.TempDir()

	// Create a fake repo package so Install succeeds.
	pkgDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)
	if err := mgr.Install("test-repo", "nginx", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	version, found, err := mgr.GetInstalledVersion("test-repo", "nginx")
	if err != nil {
		t.Fatalf("GetInstalledVersion: %v", err)
	}
	if !found {
		t.Fatal("expected package to be found")
	}
	if version != "1.0" {
		t.Fatalf("expected version 1.0, got %q", version)
	}
}

func TestGetInstalledVersionNotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	version, found, err := mgr.GetInstalledVersion("test-repo", "nonexistent")
	if err != nil {
		t.Fatalf("GetInstalledVersion: %v", err)
	}
	if found {
		t.Fatalf("expected not found, got version %q", version)
	}
}

func TestListInstalledWalksNestedDeps(t *testing.T) {
	dir := t.TempDir()

	// Create repo fixtures for both the parent and the dep's source pkg.
	for _, sp := range []struct {
		name    string
		version string
	}{
		{"myapp", "1.0"},
		{"postgres", "15.0"},
		{"metrics-exporter", "0.3"},
	} {
		repoPkgDir := filepath.Join(dir, "core", PackagesDir, sp.name)
		if err := os.MkdirAll(repoPkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll %s: %v", sp.name, err)
		}
		if err := os.WriteFile(filepath.Join(repoPkgDir, sp.version+".yaml"), []byte("image: "+sp.name+"\n"), 0600); err != nil {
			t.Fatalf("WriteFile %s: %v", sp.name, err)
		}
	}

	mgr := NewInstallManager(dir)

	// Parent
	if err := mgr.Install("core", "myapp", "myapp", "1.0", Responses{}); err != nil {
		t.Fatalf("install parent: %v", err)
	}
	// Dep one level deep
	if err := mgr.Install("core", "myapp--dep--db", "postgres", "15.0", Responses{}); err != nil {
		t.Fatalf("install dep: %v", err)
	}
	// Sub-dep two levels deep
	if err := mgr.Install("core", "myapp--dep--db--dep--metrics", "metrics-exporter", "0.3", Responses{}); err != nil {
		t.Fatalf("install sub-dep: %v", err)
	}

	got, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	// The walker must reconstruct the flat effective name for each record.
	want := map[string]bool{
		"core/myapp@1.0":                         true,
		"core/myapp--dep--db@15.0":               true,
		"core/myapp--dep--db--dep--metrics@0.3": true,
	}
	if len(got) != len(want) {
		t.Fatalf("ListInstalled returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, entry := range got {
		if !want[entry] {
			t.Errorf("unexpected entry: %q", entry)
		}
	}
}

func TestListInstalledSkipsSidecarFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a parent install record manually, no Install call — tests
	// walker robustness against stray files directly.
	pkgDir := filepath.Join(dir, InstalledDir, "core", "myapp")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: myapp\n"), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	// Sidecar files that walker must ignore.
	for _, name := range []string{"dependencies.json", "children.json", "disabled"} {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte("{}"), 0600); err != nil {
			t.Fatalf("write sidecar %s: %v", name, err)
		}
	}

	mgr := NewInstallManager(dir)
	got, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(got) != 1 || got[0] != "core/myapp@1.0" {
		t.Fatalf("ListInstalled = %v, want [core/myapp@1.0]", got)
	}
}

func TestListInstalledHandlesEmptySubpackagesDir(t *testing.T) {
	dir := t.TempDir()

	// Materialize a parent install record plus an empty subpackages/ dir
	// (as would exist briefly between a dep uninstall and walk-up cleanup,
	// or after a btrfs intermediate subvol creation without any children).
	pkgDir := filepath.Join(dir, InstalledDir, "core", "myapp")
	if err := os.MkdirAll(filepath.Join(pkgDir, SubpackagesDir), 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: myapp\n"), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	mgr := NewInstallManager(dir)
	got, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(got) != 1 || got[0] != "core/myapp@1.0" {
		t.Fatalf("ListInstalled = %v, want [core/myapp@1.0]", got)
	}
}

func TestUninstallWalksUpEmptyAncestors(t *testing.T) {
	dir := t.TempDir()

	// Build repo fixtures so Install succeeds for both source pkgs.
	for _, sp := range []struct{ name, version string }{
		{"myapp", "1.0"},
		{"postgres", "15.0"},
	} {
		repoPkgDir := filepath.Join(dir, "core", PackagesDir, sp.name)
		if err := os.MkdirAll(repoPkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repoPkgDir, sp.version+".yaml"), []byte("image: "+sp.name+"\n"), 0600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	mgr := NewInstallManager(dir)
	if err := mgr.Install("core", "myapp", "myapp", "1.0", Responses{}); err != nil {
		t.Fatalf("install parent: %v", err)
	}
	if err := mgr.Install("core", "myapp--dep--db", "postgres", "15.0", Responses{}); err != nil {
		t.Fatalf("install dep: %v", err)
	}

	// Uninstall the dep. The parent's own record remains, so cleanup
	// should drop the dep dir and the (now empty) subpackages/ parent,
	// but MUST NOT remove the parent install dir.
	if err := mgr.Uninstall("core", "myapp--dep--db", "15.0"); err != nil {
		t.Fatalf("Uninstall dep: %v", err)
	}

	parentDir := filepath.Join(dir, InstalledDir, "core", "myapp")
	if _, err := os.Stat(parentDir); err != nil {
		t.Fatalf("parent install dir unexpectedly removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parentDir, SubpackagesDir)); !os.IsNotExist(err) {
		t.Fatalf("subpackages/ dir not cleaned up: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parentDir, "1.0.yaml")); err != nil {
		t.Fatalf("parent yaml unexpectedly removed: %v", err)
	}
}

func TestGetInstalledVersionMock(t *testing.T) {
	mock := InitMockInstallManager()
	if err := mock.Install("repo", "app", "app", "2.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	version, found, err := mock.GetInstalledVersion("repo", "app")
	if err != nil {
		t.Fatalf("GetInstalledVersion: %v", err)
	}
	if !found {
		t.Fatal("expected package to be found in mock")
	}
	if version != "2.0" {
		t.Fatalf("expected version 2.0, got %q", version)
	}

	_, found, err = mock.GetInstalledVersion("repo", "missing")
	if err != nil {
		t.Fatalf("GetInstalledVersion: %v", err)
	}
	if found {
		t.Fatal("expected not found for missing package in mock")
	}
}
