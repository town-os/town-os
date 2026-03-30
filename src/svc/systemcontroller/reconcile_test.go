// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// setupReconcileRepo creates a temp directory with a repository containing the
// given packages. Each entry in pkgs maps "name/version" to the YAML content.
// Returns the RepositoryRoot and InstallManager rooted at that directory.
func setupReconcileRepo(t *testing.T, pkgs map[string]string) (*packages.RepositoryRoot, *packages.InstallManager) {
	t.Helper()

	dir := t.TempDir()

	const repoName = "repo-a"

	// Write repositories.json with a single local repo entry.
	repos := []packages.Repository{{Name: repoName, URL: url.URL{Scheme: "file", Path: dir}}}
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	err = os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600)
	if err != nil {
		t.Fatalf("write repos file: %v", err)
	}

	// Create package YAML files under <repo>/<packages>/<name>/<version>.yaml.
	for nameVersion, content := range pkgs {
		pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, filepath.Dir(nameVersion))
		err := os.MkdirAll(pkgDir, 0750)
		if err != nil {
			t.Fatalf("mkdir %s: %v", pkgDir, err)
		}
		fn := filepath.Base(nameVersion) + ".yaml"
		err = os.WriteFile(filepath.Join(pkgDir, fn), []byte(content), 0600)
		if err != nil {
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
	rr, inst := setupReconcileRepo(t, nil)
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
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	sd := systemd.InitMockManager()

	// Pre-install the package so it appears in ListInstalled.
	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("expected string arg")
	}
	if unitName != "town-os-package--repo-a-nginx-1.0.service" {
		t.Fatalf("expected unit name town-os-package--repo-a-nginx-1.0.service, got %s", unitName)
	}

	if calls[1].Method != "SetStatus" || calls[1].Args[1] != systemd.Start {
		t.Fatalf("expected SetStatus Start, got %s %v", calls[1].Method, calls[1].Args)
	}
}

func TestReconcileMultiplePackages(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
		"redis/7.0": "image: redis:7.0\n",
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	err = inst.Install("repo-a", "redis", "redis", "7.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\nvolumes:\n  data:\n    mountpoint: /var/data\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 10 {
		t.Fatalf("expected 10 filesystems, got %d: %v", len(fs), fs)
	}
	if fs[0].Name != "installed" {
		t.Fatalf("expected root subvolume installed, got %s", fs[0].Name)
	}
	if fs[1].Name != "uninstalled" {
		t.Fatalf("expected root subvolume uninstalled, got %s", fs[1].Name)
	}
	if fs[2].Name != "archives" {
		t.Fatalf("expected root subvolume archives, got %s", fs[2].Name)
	}
	if fs[3].Name != "pages" {
		t.Fatalf("expected root subvolume pages, got %s", fs[3].Name)
	}
	if fs[4].Name != "vm-images" {
		t.Fatalf("expected root subvolume vm-images, got %s", fs[4].Name)
	}
	if fs[5].Name != "user" {
		t.Fatalf("expected root subvolume user, got %s", fs[5].Name)
	}
	if fs[6].Name != "installed/repo-a" {
		t.Fatalf("expected intermediate installed/repo-a, got %s", fs[6].Name)
	}
	if fs[7].Name != "installed/repo-a/nginx" {
		t.Fatalf("expected intermediate installed/repo-a/nginx, got %s", fs[7].Name)
	}
	if fs[8].Name != "installed/repo-a/nginx/1.0" {
		t.Fatalf("expected intermediate installed/repo-a/nginx/1.0, got %s", fs[8].Name)
	}
	if fs[9].Name != "installed/repo-a/nginx/1.0/data" {
		t.Fatalf("expected volume installed/repo-a/nginx/1.0/data, got %s", fs[9].Name)
	}
}

func TestReconcileWithResponses(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:@version@\nquestions:\n  version:\n    query: Version?\n",
	})
	sd := systemd.InitMockManager()

	responses := packages.Responses{"version": "1.0"}
	err := inst.Install("repo-a", "nginx", "nginx", "1.0", responses)
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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

func TestReconcilePackageSurvivesRepoRemoval(t *testing.T) {
	// Package is installed and its repo is removed from Items, but the on-disk
	// package files remain. LoadPackage reads from disk, so reconcile should
	// still restore the package.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// Remove the repo from the RepositoryRoot Items list but leave files on disk.
	err = rr.Remove("repo-a")
	if err != nil {
		t.Fatalf("remove repo: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// Package files on disk are still readable, so package reconciles normally.
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls (package survives repo removal), got %d: %v", len(calls), calls)
	}
}

func TestReconcilePartialFailureContinues(t *testing.T) {
	// Two packages installed. First one's repo is missing, second is fine.
	// Reconcile should skip the broken one and restore the good one.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"redis/7.0": "image: redis:7.0\n",
	})
	sd := systemd.InitMockManager()

	// Create a regular file in the installed directory to simulate a hard-linked
	// installed package whose source repo no longer exists on disk.
	nginxDir := filepath.Join(rr.BaseDir, packages.InstalledDir, "missing-repo", "nginx")
	err := os.MkdirAll(nginxDir, 0750)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(filepath.Join(nginxDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600)
	if err != nil {
		t.Fatalf("write installed file: %v", err)
	}

	// Install redis properly.
	err = inst.Install("repo-a", "redis", "redis", "7.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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

	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("expected string arg")
	}
	if unitName != "town-os-package--repo-a-redis-7.0.service" {
		t.Fatalf("expected town-os-package--repo-a-redis-7.0.service, got %s", unitName)
	}
}

func TestReconcileNilManagers(t *testing.T) {
	// Reconcile should work when Storage and Systemd are nil.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func TestReconcileDisabledPackageNotStarted(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = inst.SetDisabled("repo-a", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
		"redis/7.0": "image: redis:7.0\n",
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	err = inst.Install("repo-a", "redis", "redis", "7.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	// Disable nginx only.
	err = inst.SetDisabled("repo-a", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
		"nginx/2.0": "image: nginx:2.0\n",
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx 1.0: %v", err)
	}
	err = inst.Install("repo-a", "nginx", "nginx", "2.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx 2.0: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
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

	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string arg")
	}
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

// setupMultiRepoReconcile creates a temp directory with two repositories
// (repo-a and repo-b), each containing the packages specified. Returns the
// RepositoryRoot and InstallManager.
func setupMultiRepoReconcile(t *testing.T, pkgsA, pkgsB map[string]string) (*packages.RepositoryRoot, *packages.InstallManager) {
	t.Helper()

	dir := t.TempDir()

	repos := []packages.Repository{
		{Name: "repo-a", URL: url.URL{Scheme: "file", Path: dir}},
		{Name: "repo-b", URL: url.URL{Scheme: "file", Path: dir}},
	}
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	err = os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600)
	if err != nil {
		t.Fatalf("write repos file: %v", err)
	}

	for repoName, pkgs := range map[string]map[string]string{"repo-a": pkgsA, "repo-b": pkgsB} {
		for nameVersion, content := range pkgs {
			pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, filepath.Dir(nameVersion))
			err := os.MkdirAll(pkgDir, 0750)
			if err != nil {
				t.Fatalf("mkdir %s: %v", pkgDir, err)
			}
			fn := filepath.Base(nameVersion) + ".yaml"
			err = os.WriteFile(filepath.Join(pkgDir, fn), []byte(content), 0600)
			if err != nil {
				t.Fatalf("write %s: %v", fn, err)
			}
		}
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("init repo root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	return rr, inst
}

func TestReconcileMultiRepoSamePackageName(t *testing.T) {
	rr, inst := setupMultiRepoReconcile(t,
		map[string]string{"nginx/1.0": "image: nginx:1.0\n"},
		map[string]string{"nginx/1.0": "image: nginx:1.0-alt\n"},
	)
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-a/nginx: %v", err)
	}
	err = inst.Install("repo-b", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-b/nginx: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// 2 packages * 2 calls each (InstallUnit + SetStatus Start) = 4
	if len(calls) != 4 {
		t.Fatalf("expected 4 systemd calls, got %d: %v", len(calls), calls)
	}

	// Collect unit names from InstallUnit calls.
	unitNames := map[string]bool{}
	for _, c := range calls {
		if c.Method == "InstallUnit" {
			name, ok := c.Args[0].(string)
			if !ok {
				t.Fatal("expected string arg")
			}
			unitNames[name] = true
		}
	}

	wantA := "town-os-package--repo-a-nginx-1.0.service"
	wantB := "town-os-package--repo-b-nginx-1.0.service"
	if !unitNames[wantA] {
		t.Fatalf("expected unit %s, got %v", wantA, unitNames)
	}
	if !unitNames[wantB] {
		t.Fatalf("expected unit %s, got %v", wantB, unitNames)
	}
}

func TestReconcileMultiRepoVolumePaths(t *testing.T) {
	rr, inst := setupMultiRepoReconcile(t,
		map[string]string{"nginx/1.0": "image: nginx:1.0\nvolumes:\n  data:\n    mountpoint: /var/data\n"},
		map[string]string{"nginx/1.0": "image: nginx:1.0-alt\nvolumes:\n  data:\n    mountpoint: /var/data\n"},
	)
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-a/nginx: %v", err)
	}
	err = inst.Install("repo-b", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-b/nginx: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	fs := controller.GetFilesystems()
	// Root: installed, uninstalled (2)
	// repo-a: installed/repo-a, installed/repo-a/nginx, installed/repo-a/nginx/1.0, installed/repo-a/nginx/1.0/data (4)
	// repo-b: installed/repo-b, installed/repo-b/nginx, installed/repo-b/nginx/1.0, installed/repo-b/nginx/1.0/data (4)
	// Total: 10
	fsNames := map[string]bool{}
	for _, f := range fs {
		fsNames[f.Name] = true
	}

	wantA := "installed/repo-a/nginx/1.0/data"
	wantB := "installed/repo-b/nginx/1.0/data"
	if !fsNames[wantA] {
		t.Fatalf("expected volume %s, got %v", wantA, fsNames)
	}
	if !fsNames[wantB] {
		t.Fatalf("expected volume %s, got %v", wantB, fsNames)
	}
}

func TestReconcileMultiRepoDisabledIsolation(t *testing.T) {
	rr, inst := setupMultiRepoReconcile(t,
		map[string]string{"nginx/1.0": "image: nginx:1.0\n"},
		map[string]string{"nginx/1.0": "image: nginx:1.0-alt\n"},
	)
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-a/nginx: %v", err)
	}
	err = inst.Install("repo-b", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-b/nginx: %v", err)
	}

	// Disable only repo-a's nginx.
	err = inst.SetDisabled("repo-a", "nginx", true)
	if err != nil {
		t.Fatalf("SetDisabled repo-a/nginx: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// repo-a: InstallUnit only (disabled, no Start) = 1
	// repo-b: InstallUnit + SetStatus Start = 2
	// Total: 3
	if len(calls) != 3 {
		t.Fatalf("expected 3 systemd calls, got %d: %v", len(calls), calls)
	}

	// Verify repo-b's unit gets started but repo-a's does not.
	startedUnits := map[string]bool{}
	for _, c := range calls {
		if c.Method == "SetStatus" && c.Args[1] == systemd.Start {
			name, ok := c.Args[0].(string)
			if !ok {
				t.Fatal("expected string arg")
			}
			startedUnits[name] = true
		}
	}

	repoAUnit := "town-os-package--repo-a-nginx-1.0.service"
	repoBUnit := "town-os-package--repo-b-nginx-1.0.service"
	if startedUnits[repoAUnit] {
		t.Fatalf("repo-a unit %s should not be started (disabled)", repoAUnit)
	}
	if !startedUnits[repoBUnit] {
		t.Fatalf("repo-b unit %s should be started", repoBUnit)
	}
}

func TestReconcileCreatesRootVolumes(t *testing.T) {
	rr, inst := setupReconcileRepo(t, nil)
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
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
	if len(fs) != 6 {
		t.Fatalf("expected 6 root filesystems, got %d: %v", len(fs), fs)
	}
	if fs[0].Name != "installed" {
		t.Fatalf("expected root subvolume installed, got %s", fs[0].Name)
	}
	if fs[1].Name != "uninstalled" {
		t.Fatalf("expected root subvolume uninstalled, got %s", fs[1].Name)
	}
	if fs[2].Name != "archives" {
		t.Fatalf("expected root subvolume archives, got %s", fs[2].Name)
	}
	if fs[3].Name != "pages" {
		t.Fatalf("expected root subvolume pages, got %s", fs[3].Name)
	}
	if fs[4].Name != "vm-images" {
		t.Fatalf("expected root subvolume vm-images, got %s", fs[4].Name)
	}
	if fs[5].Name != "user" {
		t.Fatalf("expected root subvolume user, got %s", fs[5].Name)
	}

	// No packages installed, so no systemd calls should have been made.
	calls := sd.GetCalls()
	if len(calls) != 0 {
		t.Fatalf("expected no systemd calls, got %d", len(calls))
	}
}

func TestReconcileWithGitSeedVolume(t *testing.T) {
	// Package with a git seed volume compiles and reconciles without error.
	// The git clone will fail (invalid URL / no git in test env) but should
	// be logged and skipped, matching auto-archive behavior.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"myapp/1.0": "image: nginx:1.0\nvolumes:\n  config:\n    mountpoint: /config\n    git: https://invalid.example.com/nonexistent/repo.git\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	err := inst.Install("repo-a", "myapp", "myapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// BtrfsBasePath points to a temp dir so the git clone target won't exist
	// (ReadDir will fail), which means the clone is skipped gracefully.
	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// InstallUnit + Start = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}
}

func TestReconcileWithMultipleGitSeedVolumes(t *testing.T) {
	// Package with multiple git seed volumes reconciles without error.
	pkgYAML := `image: nginx:1.0
volumes:
  config:
    mountpoint: /config
    git: https://invalid.example.com/config.git
  templates:
    mountpoint: /templates
    git: https://invalid.example.com/templates.git
  data:
    mountpoint: /data
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"webapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	err := inst.Install("repo-a", "webapp", "webapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// InstallUnit + Start = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}
}

func TestReconcileGitSeedSkipsNonEmptyDir(t *testing.T) {
	// When the volume directory already has contents, git seed should be
	// skipped (clone only into empty directories).
	rr, inst := setupReconcileRepo(t, map[string]string{
		"myapp/1.0": "image: nginx:1.0\nvolumes:\n  config:\n    mountpoint: /config\n    git: https://invalid.example.com/nonexistent/repo.git\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	err := inst.Install("repo-a", "myapp", "myapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// Create the target volume directory with a file so it is non-empty.
	btrfsBase := t.TempDir()
	volDir := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "myapp", "1.0", "config")
	if err := os.MkdirAll(volDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(volDir, "existing.txt"), []byte("data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Reconcile should succeed because it skips non-empty dirs rather than
	// attempting to clone into them.
	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify the existing file was not overwritten.
	data, err := os.ReadFile(filepath.Join(volDir, "existing.txt"))
	if err != nil {
		t.Fatalf("expected existing.txt to remain: %v", err)
	}
	if string(data) != "data" {
		t.Fatalf("expected 'data', got %q", data)
	}
}

func TestReconcileWithTemplates(t *testing.T) {
	pkgYAML := `image: nginx:1.0
volumes:
  config:
    mountpoint: /etc/nginx
questions:
  hostname:
    query: "Hostname?"
templates:
  nginx_conf:
    volume: config
    path: nginx.conf
    content: "server_name {{.Responses.hostname}};"
  readme:
    volume: config
    path: info.txt
    content: "{{.Package.Name}} v{{.Package.Version}}"
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()

	responses := packages.Responses{"hostname": "example.com"}
	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", responses); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify template files were written.
	nginxConf := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config", "nginx.conf")
	content, err := os.ReadFile(nginxConf)
	if err != nil {
		t.Fatalf("expected nginx.conf: %v", err)
	}
	if string(content) != "server_name example.com;" {
		t.Fatalf("expected 'server_name example.com;', got %q", string(content))
	}

	readme := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config", "info.txt")
	content, err = os.ReadFile(readme)
	if err != nil {
		t.Fatalf("expected info.txt: %v", err)
	}
	if string(content) != "nginx v1.0" {
		t.Fatalf("expected 'nginx v1.0', got %q", string(content))
	}
}

func TestReconcileTemplatesDoNotOverwriteExisting(t *testing.T) {
	pkgYAML := `image: nginx:1.0
volumes:
  config:
    mountpoint: /etc/nginx
questions: {}
templates:
  conf:
    volume: config
    path: nginx.conf
    content: "new content"
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()

	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// Pre-create the template target with existing content.
	configDir := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "config")
	if err := os.MkdirAll(configDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := "user-modified config"
	if err := os.WriteFile(filepath.Join(configDir, "nginx.conf"), []byte(existing), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Existing file should not be overwritten.
	content, err := os.ReadFile(filepath.Join(configDir, "nginx.conf"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != existing {
		t.Fatalf("expected existing content preserved, got %q", string(content))
	}
}

func TestReconcileWithoutTemplatesNoFiles(t *testing.T) {
	// Packages without templates should reconcile normally.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\nvolumes:\n  data:\n    mountpoint: /var/data\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()

	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// No template files should exist in the data volume dir.
	dataDir := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "nginx", "1.0", "data")
	entries, err := os.ReadDir(dataDir)
	if err == nil && len(entries) > 0 {
		t.Fatalf("expected no template files in data dir, got %d entries", len(entries))
	}
}

func TestReconcileGitSeedVolumeWithoutGitSkipped(t *testing.T) {
	// Volumes without a git field are not cloned.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"myapp/1.0": "image: nginx:1.0\nvolumes:\n  data:\n    mountpoint: /data\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	err := inst.Install("repo-a", "myapp", "myapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	btrfsBase := t.TempDir()
	// Create an empty volume dir — without git field, no clone should happen.
	volDir := filepath.Join(btrfsBase, PackagesVolumePrefix, "repo-a", "myapp", "1.0", "data")
	if err := os.MkdirAll(volDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Volume should still be empty — no clone was attempted.
	entries, err := os.ReadDir(volDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty dir, got %d entries", len(entries))
	}
}

func TestReconcilePagesSubvolumesAndSymlinks(t *testing.T) {
	rr, inst := setupReconcileRepo(t, nil)
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}

	pagesMgr := account.InitMockPagesManager()
	_, _ = pagesMgr.Create("alpha-site", "", "", "alpha.example.com", account.PageSourceArchive, "", "")
	_, _ = pagesMgr.Create("beta-site", "", "", "beta.example.com", account.PageSourceArchive, "", "")

	btrfsBase := t.TempDir()

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		PagesManager:   pagesMgr,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify subvolumes were created: 4 root + 2 page subvolumes = 6.
	fs := controller.GetFilesystems()
	fsNames := map[string]bool{}
	for _, f := range fs {
		fsNames[f.Name] = true
	}
	if !fsNames["pages/alpha-site"] {
		t.Fatal("expected pages/alpha-site subvolume")
	}
	if !fsNames["pages/beta-site"] {
		t.Fatal("expected pages/beta-site subvolume")
	}

	// Verify symlinks were created.
	for _, name := range []string{"alpha-site", "beta-site"} {
		linkPath := filepath.Join(btrfsBase, PagesWebrootDir, name)
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("Readlink %s: %v", name, err)
		}
		expected := "/data/pages/" + name
		if target != expected {
			t.Fatalf("expected symlink target %q for %s, got %q", expected, name, target)
		}
	}
}

func TestReconcilePagesInstallsCaddyUnit(t *testing.T) {
	rr, inst := setupReconcileRepo(t, nil)
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	pagesMgr := account.InitMockPagesManager()

	btrfsBase := t.TempDir()

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		PagesManager:   pagesMgr,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()

	// Should have InstallUnit + SetStatus(Start) for the Caddy unit.
	var foundInstall, foundStart bool
	for _, c := range calls {
		if c.Method == "InstallUnit" {
			name, ok := c.Args[0].(string)
			if ok && name == PagesUnitName {
				foundInstall = true
			}
		}
		if c.Method == "SetStatus" {
			name, ok := c.Args[0].(string)
			if ok && name == PagesUnitName && c.Args[1] == systemd.Start {
				foundStart = true
			}
		}
	}

	if !foundInstall {
		t.Fatal("expected InstallUnit call for Caddy unit")
	}
	if !foundStart {
		t.Fatal("expected SetStatus(Start) call for Caddy unit")
	}

	// Verify Caddyfile was written.
	caddyPath := filepath.Join(btrfsBase, PagesCaddyDir, "Caddyfile")
	if _, err := os.Stat(caddyPath); err != nil {
		t.Fatalf("expected Caddyfile at %s: %v", caddyPath, err)
	}
}

func TestReconcileVMPackage(t *testing.T) {
	pkgYAML := `vm:
  image: debian.raw
  memory: 2gb
  cpus: 2
network:
  external:
    8022: 22
  internal: {}
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"debian-vm/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()

	err := inst.Install("repo-a", "debian-vm", "debian-vm", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// VM with 1 external port:
	//   InstallUnit(service) + InstallUnit(socket) + InstallUnit(networkcontroller)
	//   + Enable(socket) + Enable(networkcontroller) + Start(NC) + Start(service) = 7
	if len(calls) != 7 {
		t.Fatalf("expected 7 systemd calls, got %d: %v", len(calls), calls)
	}

	// First call should install the service unit.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %s", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("expected string arg")
	}
	if unitName != "town-os-package--repo-a-debian-vm-1.0.service" {
		t.Fatalf("expected unit name town-os-package--repo-a-debian-vm-1.0.service, got %s", unitName)
	}

	// Service unit content should reference qemu, not podman.
	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string content arg")
	}
	if !strings.Contains(unitContent, "qemu-system-x86_64") {
		t.Fatal("VM service unit should contain qemu-system-x86_64")
	}
	if strings.Contains(unitContent, "podman") {
		t.Fatal("VM service unit should not reference podman")
	}
	if !strings.Contains(unitContent, "-m 2048") {
		t.Fatalf("VM service unit missing -m 2048, got:\n%s", unitContent)
	}
	if !strings.Contains(unitContent, "-smp 2") {
		t.Fatalf("VM service unit missing -smp 2, got:\n%s", unitContent)
	}
	if !strings.Contains(unitContent, "hostfwd=tcp::8022-:22") {
		t.Fatalf("VM service unit missing port forwarding, got:\n%s", unitContent)
	}

	// Last call should be Start.
	lastCall := calls[len(calls)-1]
	if lastCall.Method != "SetStatus" || lastCall.Args[1] != systemd.Start {
		t.Fatalf("last call: expected SetStatus Start, got %s %v", lastCall.Method, lastCall.Args)
	}
}

func TestReconcileVMPackageNoPorts(t *testing.T) {
	pkgYAML := `vm:
  image: headless.raw
  memory: 1gb
  cpus: 1
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"headless/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	err := inst.Install("repo-a", "headless", "headless", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// VM with no ports: InstallUnit(service) + Start(service) = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string content arg")
	}
	if !strings.Contains(unitContent, "qemu-system-x86_64") {
		t.Fatal("VM service unit should contain qemu-system-x86_64")
	}
	if !strings.Contains(unitContent, "-m 1024") {
		t.Fatalf("VM service unit missing -m 1024, got:\n%s", unitContent)
	}
}

func TestReconcileProtonPackage(t *testing.T) {
	// Proton package without explicit image URL: the reconciler should use
	// the proton_image setting. The proton app extraction will fail because
	// podman is not available in test, but the reconcile should still install
	// the systemd unit.
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
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	// Use a mock settings manager to provide the proton_image setting.
	settings := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// InstallUnit + Start = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	// Verify the unit content contains the proton image and command.
	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string arg for unit content")
	}
	if !strings.Contains(unitContent, "ghcr.io/town-os/proton-runner:latest") {
		t.Fatalf("expected proton image in unit content, got:\n%s", unitContent)
	}
	if !strings.Contains(unitContent, "proton") {
		t.Fatalf("expected proton command in unit content, got:\n%s", unitContent)
	}
}

func TestReconcileProtonPackageNilSettings(t *testing.T) {
	// When no settings manager is provided, the proton image should be empty.
	// The reconcile should still succeed (unit installs with empty image).
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
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// No SettingsMgr — reconcileProtonImage returns "".
	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// InstallUnit + Start = 2
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}
}

func TestReconcileProtonPackageDisabled(t *testing.T) {
	// Disabled proton package should install unit but not start it.
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
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	settings := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = inst.SetDisabled("repo-a", "winapp", true)
	if err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
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

func TestReconcileProtonPackageWithStorage(t *testing.T) {
	// Proton package with storage should create volumes and attempt app extraction.
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
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"winapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}

	settings := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	err := inst.Install("repo-a", "winapp", "winapp", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		SettingsMgr:    settings,
		BtrfsBasePath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify volumes were created: 4 root + intermediate dirs + 2 package volumes.
	fs := controller.GetFilesystems()
	fsNames := map[string]bool{}
	for _, f := range fs {
		fsNames[f.Name] = true
	}
	if !fsNames["installed/repo-a/winapp/1.0/app"] {
		t.Fatal("expected app volume")
	}
	if !fsNames["installed/repo-a/winapp/1.0/compatdata"] {
		t.Fatal("expected compatdata volume")
	}

	// Systemd should have InstallUnit + Start.
	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}
}

func TestReconcileDNS(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\nnetwork:\n  domains:\n    - www\n",
		"redis/7.0": "image: redis:7.0\n",
	})

	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	if err := inst.Install("repo-a", "redis", "redis", "7.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install redis: %v", err)
	}

	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{
		values: map[string]string{"dns_tld": "lan"},
	}

	err := ReconcileDNS(context.Background(), ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	})
	if err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	calls := mock.GetCalls()

	// Expect: AddAuthoritativeZone, AddRecord(SOA), AddRecord(NS), AddRecord(A for ns1)
	// Then for each package: AddRecord(A for pkg.repo.tld.)
	// nginx has an extra domain "www", so: AddRecord(A for nginx.repo-a.lan.) + AddRecord(A for www.nginx.repo-a.lan.)
	// redis: AddRecord(A for redis.repo-a.lan.)

	var addAuthZoneCalls int
	var addRecordCalls int
	for _, c := range calls {
		switch c.Method {
		case "AddAuthoritativeZone":
			addAuthZoneCalls++
			zone, ok := c.Args[0].(string)
			if !ok {
				t.Fatal("expected string arg for AddAuthoritativeZone")
			}
			if zone != "lan." {
				t.Fatalf("expected zone lan., got %s", zone)
			}
		case "AddRecord":
			addRecordCalls++
		}
	}

	if addAuthZoneCalls != 1 {
		t.Fatalf("expected 1 AddAuthoritativeZone call, got %d", addAuthZoneCalls)
	}

	// SOA + NS + A(ns1) + A(nginx.repo-a.lan.) + A(www.nginx.repo-a.lan.) + A(redis.repo-a.lan.) = 6
	if addRecordCalls != 6 {
		t.Fatalf("expected 6 AddRecord calls, got %d", addRecordCalls)
	}
}

func TestReconcileDNSNoPackages(t *testing.T) {
	rr, inst := setupReconcileRepo(t, nil)

	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{
		values: map[string]string{},
	}

	err := ReconcileDNS(context.Background(), ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	calls := mock.GetCalls()

	// Only TLD setup: AddAuthoritativeZone + SOA + NS + A(ns1) = 4 calls
	var addAuthZoneCalls int
	for _, c := range calls {
		if c.Method == "AddAuthoritativeZone" {
			addAuthZoneCalls++
			zone, ok := c.Args[0].(string)
			if !ok {
				t.Fatal("expected string arg")
			}
			// Default TLD when not set
			if zone != "home." {
				t.Fatalf("expected default zone home., got %s", zone)
			}
		}
	}

	if addAuthZoneCalls != 1 {
		t.Fatalf("expected 1 AddAuthoritativeZone call, got %d", addAuthZoneCalls)
	}
}

func TestReconcileCompileWithContextInternalOnly(t *testing.T) {
	pkgYAML := `image: nginx:1.0
environment:
  DNS_NAME: "@PACKAGE_DNS@"
  INTERNAL_HOST: "@LOCAL_INTERNAL_HOST@"
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"webapp/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "webapp", "webapp", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	settings := &mockSettingsManager{
		values: map[string]string{"dns_tld": "lan"},
	}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.50",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string arg for unit content")
	}

	// Verify template substitutions in the unit content.
	if !strings.Contains(unitContent, "webapp.repo-a.lan") {
		t.Fatalf("expected PACKAGE_DNS substitution 'webapp.repo-a.lan' in unit content, got:\n%s", unitContent)
	}
	if !strings.Contains(unitContent, "192.168.1.50") {
		t.Fatalf("expected LOCAL_INTERNAL_HOST substitution '192.168.1.50' in unit content, got:\n%s", unitContent)
	}
}

// mockSettingsManager is a minimal in-memory settings manager for tests.
type mockSettingsManager struct {
	values map[string]string
}

func (m *mockSettingsManager) Get(key string) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", fmt.Errorf("setting %q not found", key)
	}
	return v, nil
}

func (m *mockSettingsManager) Set(key, value string) error {
	m.values[key] = value
	return nil
}

func (m *mockSettingsManager) List() (map[string]string, error) {
	out := make(map[string]string, len(m.values))
	maps.Copy(out, m.values)
	return out, nil
}

func TestReconcileDNSTLDDefault(t *testing.T) {
	got := reconcileDNSTLD(nil)
	if got != "home" {
		t.Fatalf("expected %q, got %q", "home", got)
	}
}

func TestReconcileDNSTLDFromSettings(t *testing.T) {
	mgr := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}
	got := reconcileDNSTLD(mgr)
	if got != "lan" {
		t.Fatalf("expected %q, got %q", "lan", got)
	}
}

func TestReconcileCompileWithContext(t *testing.T) {
	yaml := `
image: nginx:1.0
environment:
  MY_DNS: "@PACKAGE_DNS@"
  MY_EXT: "@LOCAL_EXTERNAL_HOST@"
  MY_INT: "@LOCAL_INTERNAL_HOST@"
notes:
  url:
    value: "http://@PACKAGE_DNS@/admin"
    type: url
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": yaml,
	})
	sd := systemd.InitMockManager()

	err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	settingsMgr := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}

	err = Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
		ExternalIP:     "1.2.3.4",
		InternalIP:     "192.168.1.1",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify the package was compiled with context by checking the systemd
	// unit content contains the substituted values. Load the package again
	// and compile with the same context to get expected values.
	ip, err := rr.LoadPackage("repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}

	compiled, err := ip.CompileWithContext(packages.Responses{}, packages.CompileContext{
		ExternalHost: "1.2.3.4",
		InternalHost: "192.168.1.1",
		PackageDNS:   "nginx.repo-a.lan",
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if compiled.Environment["MY_DNS"] != "nginx.repo-a.lan" {
		t.Fatalf("expected MY_DNS=%q, got %q", "nginx.repo-a.lan", compiled.Environment["MY_DNS"])
	}
	if compiled.Environment["MY_EXT"] != "1.2.3.4" {
		t.Fatalf("expected MY_EXT=%q, got %q", "1.2.3.4", compiled.Environment["MY_EXT"])
	}
	if compiled.Environment["MY_INT"] != "192.168.1.1" {
		t.Fatalf("expected MY_INT=%q, got %q", "192.168.1.1", compiled.Environment["MY_INT"])
	}
	if compiled.Notes["url"] != "http://nginx.repo-a.lan/admin" {
		t.Fatalf("expected notes url=%q, got %q", "http://nginx.repo-a.lan/admin", compiled.Notes["url"])
	}
}

func TestReconcileVersionChangedRestartsChangedUnits(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\nnetwork:\n  external:\n    \"8080\": \"80\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// Use the same paths for both reconciles so generated unit content
	// is identical between runs.
	btrfsBase := t.TempDir()
	netStatePath := t.TempDir()

	// First reconcile: installs units (no version change).
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       netStatePath,
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Clear calls so we can observe what the second reconcile does.
	sd.ClearCalls()

	// Second reconcile with VersionChanged=true. The units are already
	// installed with the same content, so no restarts should happen
	// (content hasn't changed).
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       netStatePath,
		VersionChanged:         true,
	}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	// No Restart calls expected since content is identical.
	for _, call := range sd.GetCalls() {
		if call.Method == "SetStatus" {
			action, ok := call.Args[1].(systemd.StatusAction)
			if ok && action == systemd.Restart {
				t.Fatalf("unexpected Restart call for %v — content unchanged", call.Args[0])
			}
		}
	}
}

func TestReconcileVersionChangedRestartsWhenContentDiffers(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\nnetwork:\n  external:\n    \"8080\": \"80\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// First reconcile installs units.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Tamper with the installed unit content to simulate a version change.
	svcUnit := systemd.UnitName("repo-a", "nginx", "1.0")
	sd.InstalledUnits[svcUnit] = "old content that differs"

	sd.ClearCalls()

	// Second reconcile with VersionChanged=true. Content differs, so
	// the service should be restarted.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         true,
	}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	restarted := false
	for _, call := range sd.GetCalls() {
		if call.Method == "SetStatus" {
			name, _ := call.Args[0].(string)
			action, _ := call.Args[1].(systemd.StatusAction)
			if name == svcUnit && action == systemd.Restart {
				restarted = true
			}
		}
	}
	if !restarted {
		t.Fatal("expected service unit to be restarted when content differs")
	}
}

func TestReconcileVersionChangedRunsPostUpdate(t *testing.T) {
	t.Parallel()
	rr, inst := setupReconcileRepo(t, map[string]string{
		"postgres/16.0": "image: postgres:16\nnetwork:\n  external:\n    \"5432\": \"5432\"\npost_update:\n  - \"pg_upgrade --check\"\n  - \"pg_upgrade\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "postgres", "postgres", "16.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// First reconcile installs units.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Tamper to simulate content change.
	svcUnit := systemd.UnitName("repo-a", "postgres", "16.0")
	sd.InstalledUnits[svcUnit] = "old content"
	sd.ClearCalls()

	// Track post-update calls.
	var postUpdateCalls []struct {
		container string
		command   string
	}

	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         true,
		PostUpdateExec: func(_ context.Context, containerName string, command string) error {
			postUpdateCalls = append(postUpdateCalls, struct {
				container string
				command   string
			}{containerName, command})
			return nil
		},
	}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	if len(postUpdateCalls) != 2 {
		t.Fatalf("expected 2 post-update calls, got %d", len(postUpdateCalls))
	}
	expectedContainer := systemd.ContainerName("repo-a", "postgres", "16.0")
	if postUpdateCalls[0].container != expectedContainer {
		t.Fatalf("expected container %q, got %q", expectedContainer, postUpdateCalls[0].container)
	}
	if postUpdateCalls[0].command != "pg_upgrade --check" {
		t.Fatalf("expected command 'pg_upgrade --check', got %q", postUpdateCalls[0].command)
	}
	if postUpdateCalls[1].command != "pg_upgrade" {
		t.Fatalf("expected command 'pg_upgrade', got %q", postUpdateCalls[1].command)
	}
}

func TestReconcilePostUpdateNotRunWhenNoVersionChange(t *testing.T) {
	t.Parallel()
	rr, inst := setupReconcileRepo(t, map[string]string{
		"postgres/16.0": "image: postgres:16\nnetwork:\n  external:\n    \"5432\": \"5432\"\npost_update:\n  - \"pg_upgrade\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "postgres", "postgres", "16.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	called := false
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         false,
		PostUpdateExec: func(_ context.Context, _ string, _ string) error {
			called = true
			return nil
		},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if called {
		t.Fatal("post-update should not run when VersionChanged is false")
	}
}

func TestReconcilePostUpdateNotRunWhenContentUnchanged(t *testing.T) {
	t.Parallel()
	rr, inst := setupReconcileRepo(t, map[string]string{
		"postgres/16.0": "image: postgres:16\nnetwork:\n  external:\n    \"5432\": \"5432\"\npost_update:\n  - \"pg_upgrade\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "postgres", "postgres", "16.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	btrfsBase := t.TempDir()
	netStatePath := t.TempDir()

	// First reconcile installs units.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       netStatePath,
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	sd.ClearCalls()

	called := false
	// Second reconcile with version changed but same content (no tamper).
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       netStatePath,
		VersionChanged:         true,
		PostUpdateExec: func(_ context.Context, _ string, _ string) error {
			called = true
			return nil
		},
	}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	if called {
		t.Fatal("post-update should not run when unit content hasn't changed")
	}
}

func TestReconcilePostUpdateFailureIsNonFatal(t *testing.T) {
	t.Parallel()
	rr, inst := setupReconcileRepo(t, map[string]string{
		"postgres/16.0": "image: postgres:16\nnetwork:\n  external:\n    \"5432\": \"5432\"\npost_update:\n  - \"failing-cmd\"\n  - \"second-cmd\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "postgres", "postgres", "16.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// First reconcile installs units.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Tamper to simulate content change.
	svcUnit := systemd.UnitName("repo-a", "postgres", "16.0")
	sd.InstalledUnits[svcUnit] = "old content"
	sd.ClearCalls()

	var calledCommands []string
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         true,
		PostUpdateExec: func(_ context.Context, _ string, command string) error {
			calledCommands = append(calledCommands, command)
			if command == "failing-cmd" {
				return errors.New("command failed")
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("reconcile should succeed even when post-update fails: %v", err)
	}

	// Both commands should have been called despite the first one failing.
	if len(calledCommands) != 2 {
		t.Fatalf("expected 2 commands called, got %d", len(calledCommands))
	}
	if calledCommands[0] != "failing-cmd" {
		t.Fatalf("expected 'failing-cmd', got %q", calledCommands[0])
	}
	if calledCommands[1] != "second-cmd" {
		t.Fatalf("expected 'second-cmd', got %q", calledCommands[1])
	}
}

func TestReconcilePostUpdateNilExecIsSkipped(t *testing.T) {
	t.Parallel()
	rr, inst := setupReconcileRepo(t, map[string]string{
		"postgres/16.0": "image: postgres:16\nnetwork:\n  external:\n    \"5432\": \"5432\"\npost_update:\n  - \"pg_upgrade\"\n",
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "postgres", "postgres", "16.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// First reconcile installs units.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Tamper to simulate content change.
	svcUnit := systemd.UnitName("repo-a", "postgres", "16.0")
	sd.InstalledUnits[svcUnit] = "old content"
	sd.ClearCalls()

	// Nil PostUpdateExec should not panic.
	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       t.TempDir(),
		VersionChanged:         true,
		PostUpdateExec:         nil,
	}); err != nil {
		t.Fatalf("reconcile with nil PostUpdateExec: %v", err)
	}
}

func TestReconcileDoesNotPickUpMonitoringSystemServices(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write repositories.json.
	repos := []packages.Repository{}
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("write repos file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	sd := systemd.InitMockManager()

	// Reconcile with no installed packages should install no monitoring units.
	if err := Reconcile(t.Context(), ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                storage.InitBtrFSMock(),
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "localhost/town-os-networkcontroller:local",
		NetworkStatePath:       t.TempDir(),
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Monitoring is started as system services in main.go, not via reconcile.
	// Verify no monitoring-related package units were installed.
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "monitoring") || strings.Contains(name, "prometheus") || strings.Contains(name, "node-exporter") {
			t.Fatalf("reconcile should not install monitoring units, but found %s", name)
		}
	}
}
