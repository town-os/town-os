//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestInstallProtonPackage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	// Build a bare git repo containing the proton package definition.
	localBareRepo := filepath.Join(t.TempDir(), "local.git")
	localWork := filepath.Join(t.TempDir(), "local-work")
	for _, args := range [][]string{
		{"init", "--bare", localBareRepo},
		{"clone", localBareRepo, localWork},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	pkgDir := filepath.Join(localWork, packages.PackagesDir, "winapp")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	pkgYAML := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
  args:
    - -fullscreen
volumes:
  app:
    mountpoint: /app
  compatdata:
    mountpoint: /proton-data
environment:
  STEAM_COMPAT_DATA_PATH: /proton-data
`
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"-C", localWork, "add", "."},
		{"-C", localWork, "-c", "user.name=test", "-c", "user.email=test@test", "-c", "commit.gpgsign=false", "commit", "-m", "init"},
		{"-C", localWork, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	cmd := exec.CommandContext(context.TODO(), "git", "-C", localBareRepo, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v: %s", err, out)
	}

	settingsMgr := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	repoName := "local"
	if err := c.AddRepository(context.TODO(), repoName, "file://"+localBareRepo, "", ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "winapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify installed.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "local/winapp@1.0" {
		t.Fatalf("expected local/winapp@1.0, got %s", pkgs.Entries[0])
	}

	// Verify systemd unit contains proton runner image and command.
	calls := sd.GetCalls()
	var foundUnit bool
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			content, ok := call.Args[1].(string)
			if !ok {
				continue
			}
			if strings.Contains(content, "ghcr.io/town-os/proton-runner:latest") &&
				strings.Contains(content, "proton") &&
				strings.Contains(content, "/app/myapp.exe") &&
				strings.Contains(content, "-fullscreen") &&
				strings.Contains(content, "STEAM_COMPAT_DATA_PATH=/proton-data") {
				foundUnit = true
			}
		}
	}
	if !foundUnit {
		t.Fatal("expected systemd unit with proton runner image, command, and environment")
	}
}

func TestInstallProtonPackageAndReconcile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	// Build a bare git repo containing the proton package definition.
	localBareRepo := filepath.Join(t.TempDir(), "local.git")
	localWork := filepath.Join(t.TempDir(), "local-work")
	for _, args := range [][]string{
		{"init", "--bare", localBareRepo},
		{"clone", localBareRepo, localWork},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	pkgDir := filepath.Join(localWork, packages.PackagesDir, "winapp")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	pkgYAML := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
volumes:
  app:
    mountpoint: /app
  compatdata:
    mountpoint: /proton-data
environment:
  STEAM_COMPAT_DATA_PATH: /proton-data
`
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"-C", localWork, "add", "."},
		{"-C", localWork, "-c", "user.name=test", "-c", "user.email=test@test", "-c", "commit.gpgsign=false", "commit", "-m", "init"},
		{"-C", localWork, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	cmd := exec.CommandContext(context.TODO(), "git", "-C", localBareRepo, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v: %s", err, out)
	}

	settingsMgr := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	repoName := "local"
	if err := c.AddRepository(context.TODO(), repoName, "file://"+localBareRepo, "", ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "winapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Simulate container restart: clear mock systemd state.
	sd.Calls = nil

	// Run reconciliation.
	err = systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify reconciliation re-created units with proton image.
	calls := sd.GetCalls()
	var foundUnit bool
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			content, ok := call.Args[1].(string)
			if !ok {
				continue
			}
			if strings.Contains(content, "ghcr.io/town-os/proton-runner:latest") &&
				strings.Contains(content, "proton") &&
				strings.Contains(content, "/app/myapp.exe") {
				foundUnit = true
			}
		}
	}
	if !foundUnit {
		t.Fatal("expected reconciled systemd unit with proton runner image and command")
	}

	// Verify installed state is intact.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "local/winapp@1.0" {
		t.Fatalf("expected [local/winapp@1.0], got %v", pkgs.Entries)
	}
}
