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
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	upstream "gitea.com/town-os/rolodex-dns/go"
)

// mockIngressClient records SetRoutes calls so reconcile/handler tests can
// assert the ingress was programmed without a real gRPC server.
type mockIngressClient struct {
	setCalls [][]*ingresspb.Route
}

func (m *mockIngressClient) SetRoutes(_ context.Context, routes []*ingresspb.Route) error {
	m.setCalls = append(m.setCalls, routes)
	return nil
}
func (m *mockIngressClient) AddRoute(context.Context, *ingresspb.Route) error    { return nil }
func (m *mockIngressClient) RemoveRoute(context.Context, string) error           { return nil }
func (m *mockIngressClient) ListRoutes(context.Context) ([]*ingresspb.Route, error) {
	return nil, nil
}
func (m *mockIngressClient) Close() error { return nil }

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

	// Seven root subvolumes plus the object-storage root, then the four levels
	// of the installed package's volume path.
	fs := controller.GetFilesystems()
	if len(fs) != 12 {
		t.Fatalf("expected 12 filesystems, got %d: %v", len(fs), fs)
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
	if fs[6].Name != "tls" {
		t.Fatalf("expected root subvolume tls, got %s", fs[6].Name)
	}
	if fs[7].Name != "gfeh" {
		t.Fatalf("expected root subvolume gfeh, got %s", fs[7].Name)
	}
	if fs[8].Name != "installed/repo-a" {
		t.Fatalf("expected intermediate installed/repo-a, got %s", fs[8].Name)
	}
	if fs[9].Name != "installed/repo-a/nginx" {
		t.Fatalf("expected intermediate installed/repo-a/nginx, got %s", fs[9].Name)
	}
	if fs[10].Name != "installed/repo-a/nginx/1.0" {
		t.Fatalf("expected intermediate installed/repo-a/nginx/1.0, got %s", fs[10].Name)
	}
	if fs[11].Name != "installed/repo-a/nginx/1.0/data" {
		t.Fatalf("expected volume installed/repo-a/nginx/1.0/data, got %s", fs[11].Name)
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

	// installed, uninstalled, archives, pages, vm-images, user, tls, gfeh.
	fs := controller.GetFilesystems()
	if len(fs) != 8 {
		t.Fatalf("expected 8 root filesystems, got %d: %v", len(fs), fs)
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
	if fs[6].Name != "tls" {
		t.Fatalf("expected root subvolume tls, got %s", fs[6].Name)
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
	_, _ = pagesMgr.Create("alpha-site", "", "", "alpha.example.com", account.PageSourceArchive, "", "", "")
	_, _ = pagesMgr.Create("beta-site", "", "", "beta.example.com", account.PageSourceArchive, "", "", "")

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

	// Subvolumes and symlinks are keyed by the served FQDN (the page's domain),
	// not its short name.
	fs := controller.GetFilesystems()
	fsNames := map[string]bool{}
	for _, f := range fs {
		fsNames[f.Name] = true
	}
	if !fsNames["pages/alpha.example.com"] {
		t.Fatal("expected pages/alpha.example.com subvolume")
	}
	if !fsNames["pages/beta.example.com"] {
		t.Fatal("expected pages/beta.example.com subvolume")
	}

	// Verify symlinks were created.
	for _, name := range []string{"alpha.example.com", "beta.example.com"} {
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

// TestReconcileProgramsIngress verifies the periodic reconcile programs the
// shared :443 ingress over gRPC (rather than the legacy file-mounted Caddy
// unit it used to install) when an IngressClient is configured.
func TestReconcileProgramsIngress(t *testing.T) {
	rr, inst := setupReconcileRepo(t, nil)
	sd := systemd.InitMockManager()
	mock := storage.InitBtrFSMock()

	pagesMgr := account.InitMockPagesManager()

	btrfsBase := t.TempDir()
	ic := &mockIngressClient{}

	err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		PagesManager:   pagesMgr,
		BtrfsBasePath:  btrfsBase,
		IngressClient:  ic,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(ic.setCalls) == 0 {
		t.Fatal("expected Reconcile to program the ingress via SetRoutes")
	}

	// The legacy town-os-pages :443 Caddy unit must no longer be installed —
	// the ingress is its own boot service now.
	for _, c := range sd.GetCalls() {
		if c.Method == "InstallUnit" {
			if name, ok := c.Args[0].(string); ok && name == PagesUnitName {
				t.Fatalf("reconcile should not install the legacy %s unit anymore", PagesUnitName)
			}
		}
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

	// On a first run (zone absent, no existing records) the diff-based
	// ReconcileDNS creates the zone via SetupTLD (AddAuthoritativeZone +
	// SOA + NS + A for ns1) and then adds one A record per desired
	// package FQDN. Teardown/RemoveAuthoritativeZone must NOT appear —
	// the old teardown-then-rebuild pattern caused the .home domains to
	// briefly NXDOMAIN on every systemcontroller restart.

	var addAuthZoneCalls, addRecordCalls, listRecordsCalls,
		listAuthZonesCalls, removeAuthZoneCalls, removeRecordCalls int
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
		case "ListRecords":
			listRecordsCalls++
		case "ListAuthoritativeZones":
			listAuthZonesCalls++
		case "RemoveAuthoritativeZone":
			removeAuthZoneCalls++
		case "RemoveRecord":
			removeRecordCalls++
		}
	}

	// Must not tear down the zone on a reconcile — that would break the
	// whole point of the diff-based approach.
	if removeAuthZoneCalls != 0 {
		t.Fatalf("expected 0 RemoveAuthoritativeZone calls (diff-based), got %d", removeAuthZoneCalls)
	}
	if removeRecordCalls != 0 {
		t.Fatalf("expected 0 RemoveRecord calls on first run, got %d", removeRecordCalls)
	}
	if listAuthZonesCalls != 1 {
		t.Fatalf("expected 1 ListAuthoritativeZones call, got %d", listAuthZonesCalls)
	}
	if listRecordsCalls != 1 {
		t.Fatalf("expected 1 ListRecords call, got %d", listRecordsCalls)
	}
	if addAuthZoneCalls != 1 {
		t.Fatalf("expected 1 AddAuthoritativeZone call (first-time zone setup), got %d", addAuthZoneCalls)
	}
	// SOA + NS + A(ns1) + A(nginx.repo-a.lan.) + A(www.nginx.repo-a.lan.) + A(redis.repo-a.lan.) = 6
	if addRecordCalls != 6 {
		t.Fatalf("expected 6 AddRecord calls, got %d", addRecordCalls)
	}
}

// TestReconcileDNSPublishesAAAA verifies that when the host has a global IPv6,
// ReconcileDNS publishes a parallel AAAA record for every package FQDN (and
// ns1), and that a second run is idempotent (no add/remove churn).
func TestReconcileDNSPublishesAAAA(t *testing.T) {
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
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}
	cfg := ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
		InternalIPv6:   "2001:db8::50",
	}

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	// Count AAAA AddRecord calls and confirm each carries the host v6. Expect
	// ns1 + nginx + www.nginx + redis = 4.
	var aaaa int
	for _, c := range mock.GetCalls() {
		if c.Method != "AddRecord" {
			continue
		}
		rec, ok := c.Args[0].(*upstream.DnsRecord)
		if !ok {
			t.Fatal("AddRecord arg is not *DnsRecord")
		}
		if rec.RecordType != upstream.RecordTypeAAAA {
			continue
		}
		aaaa++
		if rec.Value != "2001:db8::50" {
			t.Errorf("AAAA %s value = %q, want 2001:db8::50", rec.Name, rec.Value)
		}
	}
	if aaaa != 4 {
		t.Fatalf("expected 4 AAAA AddRecord calls (ns1 + 3 package FQDNs), got %d", aaaa)
	}

	// Second run must be a no-op: every A and AAAA already exists, nothing is
	// orphaned, so no AddRecord/RemoveRecord calls fire.
	mock.Calls = nil
	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS second run: %v", err)
	}
	for _, c := range mock.GetCalls() {
		if c.Method == "AddRecord" || c.Method == "RemoveRecord" {
			t.Errorf("second run made a %s call; expected idempotent no-op", c.Method)
		}
	}
}

// TestReconcileDNSRemovesStaleAAAAOnIPv6Change verifies the diff removes an
// AAAA whose value no longer matches the host's current IPv6 and adds the new
// one, leaving the A records alone.
func TestReconcileDNSRemovesStaleAAAAOnIPv6Change(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{"nginx/1.0": "image: nginx:1.0\n"})
	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}

	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}

	// First reconcile at an old IPv6.
	cfg := ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
		InternalIPv6:   "2001:db8::1",
	}
	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS first: %v", err)
	}

	// Second reconcile with a changed IPv6.
	mock.Calls = nil
	cfg.InternalIPv6 = "2001:db8::2"
	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS second: %v", err)
	}

	var addedNew, removedOld, touchedA bool
	for _, c := range mock.GetCalls() {
		switch c.Method {
		case "AddRecord":
			rec, ok := c.Args[0].(*upstream.DnsRecord)
			if !ok {
				t.Fatal("AddRecord arg is not *DnsRecord")
			}
			if rec.RecordType == upstream.RecordTypeAAAA && rec.Value == "2001:db8::2" {
				addedNew = true
			}
			if rec.RecordType == upstream.RecordTypeA {
				touchedA = true
			}
		case "RemoveRecord":
			opts, ok := c.Args[1].(*upstream.RemoveRecordOptions)
			if ok && opts.RecordType != nil && *opts.RecordType == upstream.RecordTypeAAAA && opts.Value == "2001:db8::1" {
				removedOld = true
			}
			if ok && opts.RecordType != nil && *opts.RecordType == upstream.RecordTypeA {
				touchedA = true
			}
		}
	}
	if !addedNew {
		t.Error("expected the new AAAA (2001:db8::2) to be added")
	}
	if !removedOld {
		t.Error("expected the stale AAAA (2001:db8::1) to be removed")
	}
	if touchedA {
		t.Error("A records must be untouched when only IPv6 changed")
	}
}

// TestRebuildDNS pins the startup / IP-change contract: RebuildDNS
// always tears down the zone and re-registers every installed package.
// Callers that want non-disruptive drift repair should use ReconcileDNS
// instead — this one WILL drop resolutions mid-rebuild.
func TestRebuildDNS(t *testing.T) {
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
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}

	if err := RebuildDNS(context.Background(), ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	}); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}

	var addAuth, removeAuth, addRecord, listRecords int
	for _, c := range mock.GetCalls() {
		switch c.Method {
		case "AddAuthoritativeZone":
			addAuth++
		case "RemoveAuthoritativeZone":
			removeAuth++
		case "AddRecord":
			addRecord++
		case "ListRecords":
			listRecords++
		}
	}
	// Teardown: ListRecords (empty on first rebuild) + RemoveAuthoritativeZone.
	if listRecords != 1 {
		t.Errorf("expected 1 ListRecords, got %d", listRecords)
	}
	if removeAuth != 1 {
		t.Errorf("expected 1 RemoveAuthoritativeZone, got %d", removeAuth)
	}
	if addAuth != 1 {
		t.Errorf("expected 1 AddAuthoritativeZone, got %d", addAuth)
	}
	// SOA + NS + A(ns1) + A(nginx) + A(www.nginx) + A(redis) = 6
	if addRecord != 6 {
		t.Errorf("expected 6 AddRecord, got %d", addRecord)
	}
}

// TestRebuildDNSRunTwiceKeepsRecordCountStable confirms the teardown
// step actually cleans up before the rebuild — a regression here would
// double-add SOA/NS records on every startup.
func TestRebuildDNSRunTwiceKeepsRecordCountStable(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}
	cfg := ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	}
	for i := range 2 {
		if err := RebuildDNS(context.Background(), cfg); err != nil {
			t.Fatalf("RebuildDNS run %d: %v", i+1, err)
		}
	}
	records, err := mock.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	// SOA + NS + A(ns1) + A(nginx.repo-a.lan.) = 4
	if len(records) != 4 {
		for _, r := range records {
			t.Logf("record: name=%s type=%v value=%s", r.Name, r.RecordType, r.Value)
		}
		t.Fatalf("expected 4 records after two rebuilds, got %d", len(records))
	}
}

// TestReconcileDNSSecondRunMakesNoChanges is the regression for the
// "services flap after systemcontroller restart" report. The first
// reconcile creates records; the second must touch nothing, so that
// .home domains stay continuously resolvable across restarts (no
// NXDOMAIN gap for clients/browsers to cache).
func TestReconcileDNSSecondRunMakesNoChanges(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}

	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}
	cfg := ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	}

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("first ReconcileDNS: %v", err)
	}
	preRunCalls := len(mock.GetCalls())

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("second ReconcileDNS: %v", err)
	}

	// Second run must only READ (ListAuthoritativeZones + ListRecords).
	// Any Add/Remove here would re-introduce the DNS flap on restart.
	var added, removed, addZone, removeZone int
	for _, c := range mock.GetCalls()[preRunCalls:] {
		switch c.Method {
		case "AddRecord":
			added++
		case "RemoveRecord":
			removed++
		case "AddAuthoritativeZone":
			addZone++
		case "RemoveAuthoritativeZone":
			removeZone++
		}
	}
	if added != 0 || removed != 0 || addZone != 0 || removeZone != 0 {
		t.Fatalf("second reconcile mutated rolodex: add=%d remove=%d addZone=%d removeZone=%d", added, removed, addZone, removeZone)
	}
}

// TestReconcileDNSRemovesStaleRecords: when a package is uninstalled
// out-of-band (its record lingers in rolodex), the next reconcile must
// garbage-collect it instead of letting it resolve to a wrong IP forever.
func TestReconcileDNSRemovesStaleRecords(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})
	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}

	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}}

	// Seed rolodex with a stale record for a package that's no longer installed.
	aType := upstream.RecordTypeA
	if err := mock.AddRecord(context.Background(), &upstream.DnsRecord{
		Name:       "ghost.repo-a.lan.",
		RecordType: aType,
		Value:      "192.168.1.100",
		Ttl:        300,
	}); err != nil {
		t.Fatalf("seed stale record: %v", err)
	}

	if err := ReconcileDNS(context.Background(), ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	}); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	records, err := mock.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	for _, r := range records {
		if r.Name == "ghost.repo-a.lan." {
			t.Fatalf("stale record still present after reconcile")
		}
	}
}

func TestReconcileDNSIdempotent(t *testing.T) {
	rr, inst := setupReconcileRepo(t, map[string]string{
		"nginx/1.0": "image: nginx:1.0\n",
	})

	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}

	mock := &rolodex.MockClient{}
	settings := &mockSettingsManager{
		values: map[string]string{"dns_tld": "lan"},
	}

	cfg := ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	}

	// Run reconcile twice to verify no duplicate records accumulate.
	for i := range 2 {
		if err := ReconcileDNS(context.Background(), cfg); err != nil {
			t.Fatalf("ReconcileDNS run %d: %v", i+1, err)
		}
	}

	// After two runs, the mock should only have records from the second run
	// (teardown clears the first run's records before re-adding).
	records, err := mock.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}

	// Expected: SOA + NS + A(ns1) + A(nginx.repo-a.lan.) = 4 records
	if len(records) != 4 {
		for _, r := range records {
			t.Logf("  record: name=%s type=%v value=%s", r.Name, r.RecordType, r.Value)
		}
		t.Fatalf("expected 4 records after 2 reconcile runs (no duplicates), got %d", len(records))
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

	// First-time setup with no packages: the diff-based ReconcileDNS
	// creates the default zone (home.) via SetupTLD and adds no A
	// records beyond the zone's own SOA/NS/ns1. No teardown, ever.
	var addAuthZoneCalls, removeAuthZoneCalls int
	for _, c := range calls {
		switch c.Method {
		case "AddAuthoritativeZone":
			addAuthZoneCalls++
			zone, ok := c.Args[0].(string)
			if !ok {
				t.Fatal("expected string arg")
			}
			// Default TLD when not set
			if zone != "home." {
				t.Fatalf("expected default zone home., got %s", zone)
			}
		case "RemoveAuthoritativeZone":
			removeAuthZoneCalls++
		}
	}

	if removeAuthZoneCalls != 0 {
		t.Fatalf("expected 0 RemoveAuthoritativeZone calls (diff-based), got %d", removeAuthZoneCalls)
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

func TestReconcileDoubleAtEscapeInEnvironment(t *testing.T) {
	// Verify that @@ produces a literal @ and @@@ produces @ + template.
	// This is the pattern used for SSH URLs like ssh://git@@@PACKAGE_DNS@.
	pkgYAML := `image: nginx:1.0
environment:
  SSH_URL: "ssh://git@@@PACKAGE_DNS@/repo"
  EMAIL: "admin@@example.com"
`
	rr, inst := setupReconcileRepo(t, map[string]string{
		"gitea/1.0": pkgYAML,
	})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "gitea", "gitea", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	settings := &mockSettingsManager{
		values: map[string]string{"dns_tld": "home"},
	}

	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
		InternalIP:     "192.168.1.100",
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 systemd call")
	}

	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string arg for unit content")
	}

	// @@@ in SSH_URL: @@ = literal @, then @PACKAGE_DNS@ = gitea.repo-a.home
	if !strings.Contains(unitContent, "ssh://git@gitea.repo-a.home/repo") {
		t.Fatalf("expected 'ssh://git@gitea.repo-a.home/repo' in unit content, got:\n%s", unitContent)
	}

	// @@ in EMAIL: @@ = literal @, "example.com" is just text
	if !strings.Contains(unitContent, "admin@example.com") {
		t.Fatalf("expected 'admin@example.com' in unit content, got:\n%s", unitContent)
	}
}

// A package installed onto a non-default network must have @PACKAGE_DNS@ in its
// unit env recompiled under that network's TLD (gitea.repo-a.fart), not the
// global dns_tld. Otherwise reconcile rewrites a fart-network gitea's
// ROOT_URL/DOMAIN to .home and restarts it with a broken URL.
func TestReconcileCompileUsesNetworkTLD(t *testing.T) {
	pkgYAML := `image: nginx:1.0
environment:
  DNS_NAME: "@PACKAGE_DNS@"
`
	rr, inst := setupReconcileRepo(t, map[string]string{"gitea/2.0": pkgYAML})
	sd := systemd.InitMockManager()

	if err := inst.Install("repo-a", "gitea", "gitea", "2.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}
	if err := inst.SaveNetwork("repo-a", "gitea", "fart"); err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}

	nm := seedNetwork(t) // fart network, TLD "fart"
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	if err := Reconcile(context.Background(), ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Systemd:        sd,
		SettingsMgr:    settings,
		NetworkMgr:     nm,
		InternalIP:     "192.168.1.50",
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 systemd call")
	}
	unitContent, ok := calls[0].Args[1].(string)
	if !ok {
		t.Fatal("expected string arg for unit content")
	}
	if !strings.Contains(unitContent, "gitea.repo-a.fart") {
		t.Fatalf("expected PACKAGE_DNS under the network TLD 'gitea.repo-a.fart', got:\n%s", unitContent)
	}
	if strings.Contains(unitContent, "gitea.repo-a.home") {
		t.Fatalf("fart-network package must not fall back to the home zone, got:\n%s", unitContent)
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

func TestReconcileVersionChangedRestartsAllUnits(t *testing.T) {
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

	// Second reconcile with VersionChanged=true. Even though unit content
	// is identical, ALL packages must be restarted because container images
	// may have been updated.
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

	// Expect Restart calls for all units (NC + service) even though
	// content is unchanged — version change triggers full restart.
	svcUnit := systemd.UnitName("repo-a", "nginx", "1.0")
	ncUnit := systemd.NetworkControllerUnitName("repo-a", "nginx", "1.0")
	restartedSvc := false
	restartedNC := false
	for _, call := range sd.GetCalls() {
		if call.Method == "SetStatus" {
			name, ok := call.Args[0].(string)
			if !ok {
				continue
			}
			action, ok := call.Args[1].(systemd.StatusAction)
			if !ok {
				continue
			}
			if action == systemd.Restart {
				if name == svcUnit {
					restartedSvc = true
				}
				if name == ncUnit {
					restartedNC = true
				}
			}
		}
	}
	if !restartedSvc {
		t.Fatal("expected service unit to be restarted on version change even with unchanged content")
	}
	if !restartedNC {
		t.Fatal("expected NC unit to be restarted on version change even with unchanged content")
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
			name, ok := call.Args[0].(string)
			if !ok {
				continue
			}
			action, ok := call.Args[1].(systemd.StatusAction)
			if !ok {
				continue
			}
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

func TestReconcilePostUpdateRunsOnVersionChangeEvenWithUnchangedContent(t *testing.T) {
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
	// Second reconcile with version changed but same content. Post-update
	// commands must still run because the system was updated (container
	// images may have changed even if the unit text is identical).
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

	if !called {
		t.Fatal("post-update should run on version change even when unit content is unchanged")
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
