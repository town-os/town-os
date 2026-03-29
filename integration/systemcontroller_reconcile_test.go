// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Reconciliation tests ---

func TestReconcileAfterInstall(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	// Add a repository and install a package via the API.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify the install created systemd units.
	// nginx 1.0 has 1 external port (8080->80) and 1 volume:
	//   InstallUnit(service) + InstallUnit(socket) + InstallUnit(networkcontroller) +
	//   Enable(socket) + Enable(networkcontroller) + Start(service) = 6
	installCalls := sd.GetCalls()
	if len(installCalls) != 6 {
		t.Fatalf("expected 6 systemd calls from install, got %d", len(installCalls))
	}

	// Simulate a container restart: clear the mock systemd state.
	// The installed state (symlinks + responses) persists but units are gone.
	sd.Calls = nil

	// Run reconciliation.
	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify reconciliation re-created all units (same 6 calls).
	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls from reconcile, got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitName != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("expected unit town-os-package--core-nginx-1.0.service, got %s", unitName)
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastCall.Method != "SetStatus" || lastAction != systemd.Start {
		t.Fatalf("last call: expected SetStatus Start, got %s %v", lastCall.Method, lastCall.Args)
	}

	// Verify installed state is intact.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected [core/nginx@1.0], got %v", pkgs.Entries)
	}
}

func TestReconcileMultiplePackagesAfterInstall(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// nginx (1 ext port): 3 InstallUnit + 2 Enable + 1 Start = 6
	// redis (1 int port): 3 InstallUnit + 2 Enable + 1 Start = 6
	// Total = 12
	if len(calls) != 12 {
		t.Fatalf("expected 12 systemd calls, got %d", len(calls))
	}
}

func TestReconcilePreservesResponses(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "myhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify responses are still accessible via the API.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if resp["hostname"] != "myhost" {
		t.Fatalf("expected hostname myhost, got %s", resp["hostname"])
	}
	if resp["port"] != "9090" {
		t.Fatalf("expected port 9090, got %s", resp["port"])
	}
}

// TestReconcilePostUpdateIntegration verifies that post_update commands are
// executed during reconcile when the version changed and unit content differs.
// Uses a local package repo with inline YAML that includes post_update.
func TestReconcilePostUpdateIntegration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoName := "localrepo"

	repos := []packages.Repository{{Name: repoName, URL: url.URL{Scheme: "file", Path: dir}}}
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("write repos: %v", err)
	}

	pkgYAML := `image: postgres:16
network:
  external:
    "5432": "5432"
post_update:
  - "echo upgrade-check"
  - "echo upgrade-done"
`
	pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, "postgres")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "16.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("init repo root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	if err := inst.Install(repoName, "postgres", "postgres", "16.0", packages.Responses{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	btrfsBase := t.TempDir()
	netStatePath := t.TempDir()

	// First reconcile (no version change).
	if err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                mock,
		Systemd:                sd,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       netStatePath,
		VersionChanged:         false,
	}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Tamper with unit content to simulate version change.
	svcUnit := systemd.UnitName(repoName, "postgres", "16.0")
	sd.InstalledUnits[svcUnit] = "old content"
	sd.ClearCalls()

	var postUpdateCalls []struct {
		container string
		command   string
	}

	if err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                mock,
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

	expectedContainer := systemd.ContainerName(repoName, "postgres", "16.0")
	if postUpdateCalls[0].container != expectedContainer {
		t.Fatalf("expected container %q, got %q", expectedContainer, postUpdateCalls[0].container)
	}
	if postUpdateCalls[0].command != "echo upgrade-check" {
		t.Fatalf("expected 'echo upgrade-check', got %q", postUpdateCalls[0].command)
	}
	if postUpdateCalls[1].command != "echo upgrade-done" {
		t.Fatalf("expected 'echo upgrade-done', got %q", postUpdateCalls[1].command)
	}
}

// TestReconcilePostUpdateNotCalledForPackagesWithout verifies that packages
// without post_update do not trigger PostUpdateExec even on version change.
func TestReconcilePostUpdateNotCalledForPackagesWithout(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// First reconcile.
	sd.Calls = nil
	if err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	// Tamper to trigger content change.
	svcUnit := systemd.UnitName("core", "nginx", "1.0")
	sd.InstalledUnits[svcUnit] = "old content"
	sd.ClearCalls()

	var postUpdateCalled bool
	if err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		VersionChanged: true,
		PostUpdateExec: func(_ context.Context, _ string, _ string) error {
			postUpdateCalled = true
			return nil
		},
	}); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	if postUpdateCalled {
		t.Fatal("post-update should not be called for packages without post_update commands")
	}
}
