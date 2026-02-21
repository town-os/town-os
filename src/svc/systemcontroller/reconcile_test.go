package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// setupReconcileRepo creates a temp directory with a repository containing the
// given packages. Each entry in pkgs maps "name/version" to the YAML content.
// Returns the RepositoryRoot and InstallManager rooted at that directory.
func setupReconcileRepo(t *testing.T, repoName string, pkgs map[string]string) (*packages.RepositoryRoot, *packages.InstallManager) {
	t.Helper()

	dir := t.TempDir()

	// Write repositories.json with a single local repo entry.
	repos := []packages.Repository{{Name: repoName, URL: url.URL{Scheme: "file", Path: dir}}}
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("write repos file: %v", err)
	}

	// Create package YAML files under <repo>/<packages>/<name>/<version>.yaml.
	for nameVersion, content := range pkgs {
		pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, filepath.Dir(nameVersion))
		if err := os.MkdirAll(pkgDir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", pkgDir, err)
		}
		fn := fmt.Sprintf("%s.yaml", filepath.Base(nameVersion))
		if err := os.WriteFile(filepath.Join(pkgDir, fn), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", fn, err)
		}
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("init repo root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	return rr, inst
}

func TestReconcileEmptyInstalled(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", nil)
	sd := systemd.InitMockManager()

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 0 {
		t.Fatalf("expected no systemd calls, got %d", len(calls))
	}
}

func TestReconcileInstalledPackage(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	sd := systemd.InitMockManager()

	// Pre-install the package so it appears in ListInstalled.
	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()

	// Expect: InstallUnit, SetStatus(Start)
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("expected InstallUnit, got %s", calls[0].Method)
	}
	unitName := calls[0].Args[0].(string)
	if unitName != "town-os-nginx.service" {
		t.Fatalf("expected unit name town-os-nginx.service, got %s", unitName)
	}

	if calls[1].Method != "SetStatus" || calls[1].Args[1] != systemd.Start {
		t.Fatalf("expected SetStatus Start, got %s %v", calls[1].Method, calls[1].Args)
	}
}

func TestReconcileMultiplePackages(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
		"redis/7.0": "image: redis:7.0\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	if err := inst.Install("repo-a", "redis", "7.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// 2 packages * 2 calls each = 4
	if len(calls) != 4 {
		t.Fatalf("expected 4 systemd calls, got %d", len(calls))
	}
}

func TestReconcileWithStorageVolumes(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\nvolumes:\n  data:\n    mountpoint: /var/data\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller := mock.Controller.(*storage.MockBtrFSController)

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 5 {
		t.Fatalf("expected 5 filesystems, got %d: %v", len(fs), fs)
	}
	if fs[0].Name != "installed" {
		t.Fatalf("expected root subvolume installed, got %s", fs[0].Name)
	}
	if fs[1].Name != "uninstalled" {
		t.Fatalf("expected root subvolume uninstalled, got %s", fs[1].Name)
	}
	if fs[2].Name != "installed/nginx" {
		t.Fatalf("expected intermediate installed/nginx, got %s", fs[2].Name)
	}
	if fs[3].Name != "installed/nginx/1.0" {
		t.Fatalf("expected intermediate installed/nginx/1.0, got %s", fs[3].Name)
	}
	if fs[4].Name != "installed/nginx/1.0/data" {
		t.Fatalf("expected volume installed/nginx/1.0/data, got %s", fs[4].Name)
	}
}

func TestReconcileWithResponses(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:@version@\nquestions:\n  version:\n    query: Version?\n",
	})
	sd := systemd.InitMockManager()

	responses := packages.Responses{"version": "1.0"}
	if err := inst.Install("repo-a", "nginx", "1.0", responses); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d", len(calls))
	}
}

func TestReconcileMissingRepoSkips(t *testing.T) {
	// Package is installed but its repo is gone — reconcile should skip it
	// gracefully instead of returning a fatal error.
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// Remove the repo from the RepositoryRoot so FindRepoForPackage fails.
	if err := rr.Remove("repo-a"); err != nil {
		t.Fatalf("remove repo: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	// Should not return an error — it logs and skips.
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 0 {
		t.Fatalf("expected no systemd calls for missing repo, got %d", len(calls))
	}
}

func TestReconcilePartialFailureContinues(t *testing.T) {
	// Two packages installed. First one's repo is missing, second is fine.
	// Reconcile should skip the broken one and restore the good one.
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"redis/7.0": "image: redis:7.0\n",
	})
	sd := systemd.InitMockManager()

	// Install nginx manually (create the symlink) but don't create its package file.
	// This simulates a package whose repo was removed.
	nginxDir := filepath.Join(rr.BaseDir, packages.InstalledDir, "nginx")
	if err := os.MkdirAll(nginxDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a dangling symlink for the installed record.
	if err := os.Symlink("../../missing-repo/packages/nginx/1.0.yaml", filepath.Join(nginxDir, "1.0.yaml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Install redis properly.
	if err := inst.Install("repo-a", "redis", "7.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// Only redis should be reconciled (2 calls).
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls (redis only), got %d", len(calls))
	}

	unitName := calls[0].Args[0].(string)
	if unitName != "town-os-redis.service" {
		t.Fatalf("expected town-os-redis.service, got %s", unitName)
	}
}

func TestReconcileNilManagers(t *testing.T) {
	// Reconcile should work when Storage and Systemd are nil.
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func TestReconcileDisabledPackageNotStarted(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	if err := inst.SetDisabled("nginx", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// Only InstallUnit, no Start.
	if len(calls) != 1 {
		t.Fatalf("expected 1 systemd call (InstallUnit only), got %d: %v", len(calls), calls)
	}
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("expected InstallUnit, got %s", calls[0].Method)
	}
}

func TestReconcileDisabledAndEnabledMixed(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
		"redis/7.0": "image: redis:7.0\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	if err := inst.Install("repo-a", "redis", "7.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	// Disable nginx only.
	if err := inst.SetDisabled("nginx", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// nginx: InstallUnit (1 call, no Start)
	// redis: InstallUnit + Start (2 calls)
	// Total: 3
	if len(calls) != 3 {
		t.Fatalf("expected 3 systemd calls, got %d: %v", len(calls), calls)
	}
}

func TestReconcileMultiVersionPicksLatest(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
		"nginx/2.0": "image: nginx:2.0\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx 1.0: %v", err)
	}
	if err := inst.Install("repo-a", "nginx", "2.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx 2.0: %v", err)
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// Only the latest version (2.0) should be reconciled: InstallUnit + Start = 2 calls.
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("expected InstallUnit, got %s", calls[0].Method)
	}

	unitContent := calls[0].Args[1].(string)
	if !strings.Contains(unitContent, "nginx@2.0") {
		t.Fatalf("expected unit content to reference nginx@2.0, got:\n%s", unitContent)
	}
	if !strings.Contains(unitContent, "nginx:2.0") {
		t.Fatalf("expected unit content to reference image nginx:2.0, got:\n%s", unitContent)
	}

	if calls[1].Method != "SetStatus" || calls[1].Args[1] != systemd.Start {
		t.Fatalf("expected SetStatus Start, got %s %v", calls[1].Method, calls[1].Args)
	}
}

func TestReconcileCreatesRootVolumes(t *testing.T) {
	rr, inst := setupReconcileRepo(t, "repo-a", nil)
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller := mock.Controller.(*storage.MockBtrFSController)

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 2 {
		t.Fatalf("expected 2 root filesystems, got %d: %v", len(fs), fs)
	}
	if fs[0].Name != "installed" {
		t.Fatalf("expected root subvolume installed, got %s", fs[0].Name)
	}
	if fs[1].Name != "uninstalled" {
		t.Fatalf("expected root subvolume uninstalled, got %s", fs[1].Name)
	}

	// No packages installed, so no systemd calls should have been made.
	calls := sd.GetCalls()
	if len(calls) != 0 {
		t.Fatalf("expected no systemd calls, got %d", len(calls))
	}
}
