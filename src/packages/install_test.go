package packages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// --- Interface conformance ---

func TestInstallManagerImplementsInstaller(t *testing.T) {
	var _ Installer = (*InstallManager)(nil)
}

func TestMockInstallManagerImplementsInstaller(t *testing.T) {
	var _ Installer = (*MockInstallManager)(nil)
}

// --- MockInstallManager Install tests ---

func TestMockInstallManagerInstall(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(m.Installed))
	}

	if m.Installed[0].Name != "nginx" || m.Installed[0].Version != "1.0" {
		t.Fatalf("expected nginx@1.0, got %s@%s", m.Installed[0].Name, m.Installed[0].Version)
	}
}

func TestMockInstallManagerInstallMultiple(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}
	if err := m.Install("test-repo", "nginx", "2.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@2.0: %v", err)
	}
	if err := m.Install("test-repo", "redis", "7.0", Responses{}); err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}

	if len(m.Installed) != 3 {
		t.Fatalf("expected 3 installed, got %d", len(m.Installed))
	}
}

func TestMockInstallManagerInstallDuplicate(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	err := m.Install("test-repo", "nginx", "1.0", Responses{})
	if err == nil {
		t.Fatal("expected error for duplicate install")
	}
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}
}

func TestMockInstallManagerInstallErrorInjection(t *testing.T) {
	m := InitMockInstallManager()
	injected := fmt.Errorf("injected error")

	m.InstallErr = injected
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	// State should not change when error is injected.
	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after error, got %d", len(m.Installed))
	}
}

// --- MockInstallManager Uninstall tests ---

func TestMockInstallManagerUninstall(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	if err := m.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Installed) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(m.Installed))
	}
}

func TestMockInstallManagerUninstallPreservesOthers(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}
	if err := m.Install("test-repo", "redis", "7.0", Responses{}); err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}

	if err := m.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall nginx@1.0: %v", err)
	}

	if len(m.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(m.Installed))
	}
	if m.Installed[0].Name != "redis" {
		t.Fatalf("expected redis to remain, got %s", m.Installed[0].Name)
	}
}

func TestMockInstallManagerUninstallNotInstalled(t *testing.T) {
	m := InitMockInstallManager()

	err := m.Uninstall("test-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for not-installed package")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestMockInstallManagerUninstallErrorInjection(t *testing.T) {
	m := InitMockInstallManager()
	injected := fmt.Errorf("injected error")

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	m.UninstallErr = injected
	if err := m.Uninstall("test-repo", "nginx", "1.0"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	// State should not change when error is injected.
	if len(m.Installed) != 1 {
		t.Fatalf("expected 1 installed after error, got %d", len(m.Installed))
	}
}

// --- MockInstallManager ListInstalled tests ---

func TestMockInstallManagerListEmpty(t *testing.T) {
	m := InitMockInstallManager()

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestMockInstallManagerList(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "2.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@2.0: %v", err)
	}
	if err := m.Install("test-repo", "redis", "7.0", Responses{}); err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	found := map[string]bool{}
	for _, p := range pkgs {
		found[p] = true
	}

	if !found["test-repo/nginx@2.0"] {
		t.Fatal("expected test-repo/nginx@2.0 in list")
	}
	if !found["test-repo/redis@7.0"] {
		t.Fatal("expected test-repo/redis@7.0 in list")
	}
}

func TestMockInstallManagerListReturnsCopy(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	pkgs[0] = "mutated"

	if m.Installed[0].Name != "nginx" {
		t.Fatal("ListInstalled should return a copy, not a reference")
	}
}

func TestMockInstallManagerListErrorInjection(t *testing.T) {
	m := InitMockInstallManager()
	injected := fmt.Errorf("injected error")

	m.ListErr = injected
	if _, err := m.ListInstalled(); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- Call log tests ---

func TestMockInstallManagerCallLog(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}
	if err := m.Install("test-repo", "redis", "7.0", Responses{}); err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}
	if _, err := m.ListInstalled(); err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if err := m.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall nginx@1.0: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}

	expected := []string{"Install", "Install", "ListInstalled", "Uninstall"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestMockInstallManagerCallLogArgs(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "2.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@2.0: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	args := calls[0].Args
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
	if args[0] != "test-repo" || args[1] != "nginx" || args[2] != "2.0" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestMockInstallManagerCallLogReturnsCopy(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	calls := m.GetCalls()
	calls[0].Method = "mutated"

	if m.Calls[0].Method != "Install" {
		t.Fatal("GetCalls should return a copy, not a reference")
	}
}

// --- Lifecycle ---

func TestMockInstallManagerLifecycle(t *testing.T) {
	m := InitMockInstallManager()

	// Start empty.
	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled (initial): %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected empty, got %d", len(pkgs))
	}

	// Install.
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}
	if err := m.Install("test-repo", "redis", "7.0", Responses{}); err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}

	pkgs, err = m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled (after install): %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d", len(pkgs))
	}

	// Uninstall one.
	if err := m.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall nginx@1.0: %v", err)
	}

	pkgs, err = m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled (after uninstall): %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 after uninstall, got %d", len(pkgs))
	}
	if pkgs[0] != "test-repo/redis@7.0" {
		t.Fatalf("expected test-repo/redis@7.0 to remain, got %s", pkgs[0])
	}

	// Re-install after uninstall.
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install nginx@1.0 (re-install): %v", err)
	}

	pkgs, err = m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled (after re-install): %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 after re-install, got %d", len(pkgs))
	}
}

// --- MockInstallManager Responses tests ---

func TestMockInstallManagerInstallStoresResponses(t *testing.T) {
	m := InitMockInstallManager()
	resp := Responses{"port": "8080", "hostname": "myhost"}

	if err := m.Install("test-repo", "nginx", "1.0", resp); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := m.GetResponses("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	if got["port"] != "8080" || got["hostname"] != "myhost" {
		t.Fatalf("expected port=8080 hostname=myhost, got %v", got)
	}
}

func TestMockInstallManagerUninstallDeletesResponses(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := m.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	_, err := m.GetResponses("test-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error after uninstall")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestMockInstallManagerGetResponsesNotInstalled(t *testing.T) {
	m := InitMockInstallManager()

	_, err := m.GetResponses("test-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for not-installed package")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestMockInstallManagerGetResponsesErrorInjection(t *testing.T) {
	m := InitMockInstallManager()
	injected := fmt.Errorf("injected error")

	if err := m.Install("test-repo", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	m.GetResponsesErr = injected
	_, err := m.GetResponses("test-repo", "nginx", "1.0")
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockInstallManagerGetResponsesReturnsCopy(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("test-repo", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := m.GetResponses("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	got["port"] = "mutated"

	original := m.StoredResponses["test-repo/nginx@1.0"]
	if original["port"] != "80" {
		t.Fatal("GetResponses should return a copy, not a reference")
	}
}

// --- InstallManager disabled state tests ---

func TestSetDisabledCreatesMarker(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	if err := mgr.SetDisabled("test-repo", "nginx", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	marker := filepath.Join(dir, InstalledDir, "test-repo", "nginx", "disabled")
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("expected disabled marker file to exist")
	}
}

func TestSetDisabledClearsMarker(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	if err := mgr.SetDisabled("test-repo", "nginx", true); err != nil {
		t.Fatalf("SetDisabled(true): %v", err)
	}

	if err := mgr.SetDisabled("test-repo", "nginx", false); err != nil {
		t.Fatalf("SetDisabled(false): %v", err)
	}

	marker := filepath.Join(dir, InstalledDir, "test-repo", "nginx", "disabled")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("expected disabled marker file to be removed")
	}
}

func TestIsDisabledTrue(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	if err := mgr.SetDisabled("test-repo", "nginx", true); err != nil {
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
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)

	if err := mgr.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := mgr.SetDisabled("test-repo", "nginx", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	if err := mgr.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// The package directory should be cleaned up entirely.
	pkgInstallDir := filepath.Join(dir, InstalledDir, "test-repo", "nginx")
	if _, err := os.Stat(pkgInstallDir); !os.IsNotExist(err) {
		t.Fatalf("expected package install directory to be removed, got err: %v", err)
	}
}

// --- InstallManager response persistence tests ---

func TestInstallManagerResponsesPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create a fake repo package so Install succeeds.
	pkgDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)
	resp := Responses{"port": "8080", "hostname": "myhost"}

	if err := mgr.Install("test-repo", "nginx", "1.0", resp); err != nil {
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
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewInstallManager(dir)

	if err := mgr.Install("test-repo", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := mgr.Uninstall("test-repo", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	_, err := mgr.GetResponses("test-repo", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error after uninstall")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}

	// Verify response directory was cleaned up.
	respDir := filepath.Join(dir, ResponsesDir, "test-repo", "nginx")
	if _, err := os.Stat(respDir); !os.IsNotExist(err) {
		t.Fatalf("expected response directory to be cleaned up, got err: %v", err)
	}
}

// --- Multi-repo MockInstallManager tests ---

func TestMockInstallManagerMultiRepoInstall(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("repo-a", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install repo-a/nginx@1.0: %v", err)
	}
	if err := m.Install("repo-b", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install repo-b/nginx@1.0: %v", err)
	}

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs))
	}

	found := map[string]bool{}
	for _, p := range pkgs {
		found[p] = true
	}

	if !found["repo-a/nginx@1.0"] {
		t.Fatal("expected repo-a/nginx@1.0 in list")
	}
	if !found["repo-b/nginx@1.0"] {
		t.Fatal("expected repo-b/nginx@1.0 in list")
	}
}

func TestMockInstallManagerMultiRepoUninstallIsolation(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("repo-a", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("Install repo-a/nginx@1.0: %v", err)
	}
	if err := m.Install("repo-b", "nginx", "1.0", Responses{"port": "9090"}); err != nil {
		t.Fatalf("Install repo-b/nginx@1.0: %v", err)
	}

	if err := m.Uninstall("repo-a", "nginx", "1.0"); err != nil {
		t.Fatalf("Uninstall repo-a/nginx@1.0: %v", err)
	}

	pkgs, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs))
	}
	if pkgs[0] != "repo-b/nginx@1.0" {
		t.Fatalf("expected repo-b/nginx@1.0, got %s", pkgs[0])
	}

	// GetResponses for repo-a should error.
	_, err = m.GetResponses("repo-a", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for uninstalled repo-a/nginx@1.0")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}

	// GetResponses for repo-b should succeed.
	resp, err := m.GetResponses("repo-b", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses repo-b: %v", err)
	}
	if resp["port"] != "9090" {
		t.Fatalf("expected port=9090, got %s", resp["port"])
	}
}

func TestMockInstallManagerMultiRepoResponsesIsolation(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("repo-a", "nginx", "1.0", Responses{"port": "80"}); err != nil {
		t.Fatalf("Install repo-a: %v", err)
	}
	if err := m.Install("repo-b", "nginx", "1.0", Responses{"port": "9090"}); err != nil {
		t.Fatalf("Install repo-b: %v", err)
	}

	respA, err := m.GetResponses("repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses repo-a: %v", err)
	}
	if respA["port"] != "80" {
		t.Fatalf("expected repo-a port=80, got %s", respA["port"])
	}

	respB, err := m.GetResponses("repo-b", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses repo-b: %v", err)
	}
	if respB["port"] != "9090" {
		t.Fatalf("expected repo-b port=9090, got %s", respB["port"])
	}
}

func TestMockInstallManagerMultiRepoDisabledIsolation(t *testing.T) {
	m := InitMockInstallManager()

	if err := m.Install("repo-a", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install repo-a: %v", err)
	}
	if err := m.Install("repo-b", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install repo-b: %v", err)
	}

	if err := m.SetDisabled("repo-a", "nginx", true); err != nil {
		t.Fatalf("SetDisabled repo-a: %v", err)
	}

	disabledA, err := m.IsDisabled("repo-a", "nginx")
	if err != nil {
		t.Fatalf("IsDisabled repo-a: %v", err)
	}
	if !disabledA {
		t.Fatal("expected repo-a/nginx to be disabled")
	}

	disabledB, err := m.IsDisabled("repo-b", "nginx")
	if err != nil {
		t.Fatalf("IsDisabled repo-b: %v", err)
	}
	if disabledB {
		t.Fatal("expected repo-b/nginx to NOT be disabled")
	}
}

// --- IsPackageChanged tests ---

func TestIsPackageChangedSameInode(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create repo package file.
	repoDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	repoFile := filepath.Join(repoDir, "1.0.yaml")
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile repo: %v", err)
	}

	// Install (creates hard link).
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if changed {
		t.Fatal("expected package to NOT be changed (same inode)")
	}
}

func TestIsPackageChangedDifferentInode(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create repo package file.
	repoDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	repoFile := filepath.Join(repoDir, "1.0.yaml")
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile repo: %v", err)
	}

	// Install (creates hard link).
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Simulate upstream change: remove and recreate the repo file (breaks hard link).
	if err := os.Remove(repoFile); err != nil {
		t.Fatalf("Remove repo file: %v", err)
	}
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.1\n"), 0644); err != nil {
		t.Fatalf("WriteFile repo updated: %v", err)
	}

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected package to be changed (different inode)")
	}
}

func TestIsPackageChangedRepoFileMissing(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create repo package file.
	repoDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	repoFile := filepath.Join(repoDir, "1.0.yaml")
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile repo: %v", err)
	}

	// Install.
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Remove repo file (simulates package removed from repo).
	if err := os.Remove(repoFile); err != nil {
		t.Fatalf("Remove repo file: %v", err)
	}

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected package to be changed (repo file missing)")
	}
}

func TestMockIsPackageChanged(t *testing.T) {
	m := InitMockInstallManager()
	m.ChangedPackages["test-repo/nginx@1.0"] = true

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected true for changed package")
	}

	changed, err = m.IsPackageChanged("test-repo", "nginx", "2.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if changed {
		t.Fatal("expected false for unchanged package")
	}
}
