// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"os"
	"path/filepath"
	"testing"
)

// --- InstallManager disabled state tests ---

func TestSetDisabledCreatesMarker(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	err := mgr.SetDisabled("test-repo", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	marker := filepath.Join(dir, InstalledDir, "test-repo", "nginx", "disabled")
	_, err = os.Stat(marker)
	if os.IsNotExist(err) {
		t.Fatal("expected disabled marker file to exist")
	}
}

func TestSetDisabledClearsMarker(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	err := mgr.SetDisabled("test-repo", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled(true): %v", err)
	}

	err = mgr.SetDisabled("test-repo", "nginx", false)
	if err != nil {
		t.Fatalf("SetDisabled(false): %v", err)
	}

	marker := filepath.Join(dir, InstalledDir, "test-repo", "nginx", "disabled")
	_, err = os.Stat(marker)
	if !os.IsNotExist(err) {
		t.Fatal("expected disabled marker file to be removed")
	}
}

func TestIsDisabledTrue(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	err := mgr.SetDisabled("test-repo", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	disabled, err := mgr.IsDisabled("test-repo", "nginx")
	if err != nil {
		t.Fatalf("IsDisabled: %v", err)
	}
	if !disabled {
		t.Fatal("expected IsDisabled to return true")
	}
}

func TestIsDisabledFalse(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	disabled, err := mgr.IsDisabled("test-repo", "nginx")
	if err != nil {
		t.Fatalf("IsDisabled: %v", err)
	}
	if disabled {
		t.Fatal("expected IsDisabled to return false")
	}
}

func TestUninstallClearsDisabledMarker(t *testing.T) {
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

	err = mgr.Install("test-repo", "nginx", "1.0", Responses{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	err = mgr.SetDisabled("test-repo", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err = mgr.Uninstall("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// The package directory should be cleaned up entirely.
	pkgInstallDir := filepath.Join(dir, InstalledDir, "test-repo", "nginx")
	_, err = os.Stat(pkgInstallDir)
	if !os.IsNotExist(err) {
		t.Fatalf("expected package install directory to be removed, got err: %v", err)
	}
}
