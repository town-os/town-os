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

	// The installed record should exist under the effective name.
	link := filepath.Join(dir, InstalledDir, "default", "myapp--dep--db", "15.0.yaml")
	if _, err := os.Stat(link); err != nil {
		t.Fatalf("installed record missing: %v", err)
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
