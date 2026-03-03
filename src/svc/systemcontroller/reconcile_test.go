package systemcontroller

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
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
	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	err = inst.Install("repo-a", "redis", "7.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
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
	if len(fs) != 8 {
		t.Fatalf("expected 8 filesystems, got %d: %v", len(fs), fs)
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
	if fs[4].Name != "installed/repo-a" {
		t.Fatalf("expected intermediate installed/repo-a, got %s", fs[4].Name)
	}
	if fs[5].Name != "installed/repo-a/nginx" {
		t.Fatalf("expected intermediate installed/repo-a/nginx, got %s", fs[5].Name)
	}
	if fs[6].Name != "installed/repo-a/nginx/1.0" {
		t.Fatalf("expected intermediate installed/repo-a/nginx/1.0, got %s", fs[6].Name)
	}
	if fs[7].Name != "installed/repo-a/nginx/1.0/data" {
		t.Fatalf("expected volume installed/repo-a/nginx/1.0/data, got %s", fs[7].Name)
	}
}

func TestReconcileWithResponses(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:@version@\nquestions:\n  version:\n    query: Version?\n",
	})
	sd := systemd.InitMockManager()

	responses := packages.Responses{"version": "1.0"}
	err := inst.Install("repo-a", "nginx", "1.0", responses)
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
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
	err = inst.Install("repo-a", "redis", "7.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	err = inst.Install("repo-a", "redis", "7.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install nginx 1.0: %v", err)
	}
	err = inst.Install("repo-a", "nginx", "2.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-a/nginx: %v", err)
	}
	err = inst.Install("repo-b", "nginx", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-a/nginx: %v", err)
	}
	err = inst.Install("repo-b", "nginx", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("pre-install repo-a/nginx: %v", err)
	}
	err = inst.Install("repo-b", "nginx", "1.0", packages.Responses{})
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
	if len(fs) != 4 {
		t.Fatalf("expected 4 root filesystems, got %d: %v", len(fs), fs)
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

	err := inst.Install("repo-a", "myapp", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "webapp", "1.0", packages.Responses{})
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

	err := inst.Install("repo-a", "myapp", "1.0", packages.Responses{})
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

func TestReconcileGitSeedVolumeWithoutGitSkipped(t *testing.T) {
	// Volumes without a git field are not cloned.
	rr, inst := setupReconcileRepo(t, map[string]string{
		"myapp/1.0": "image: nginx:1.0\nvolumes:\n  data:\n    mountpoint: /data\n",
	})
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	err := inst.Install("repo-a", "myapp", "1.0", packages.Responses{})
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
	pagesMgr.Create("alpha-site", "", "", "alpha.example.com", account.PageSourceArchive, "", "") //nolint:errcheck // test setup
	pagesMgr.Create("beta-site", "", "", "beta.example.com", account.PageSourceArchive, "", "")   //nolint:errcheck // test setup

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
