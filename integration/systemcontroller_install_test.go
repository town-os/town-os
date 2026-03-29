// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Install integration tests ---

func TestSystemControllerInstallAndListInstalled(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs.Entries[0])
	}
}

func TestSystemControllerInstallAndGetResponses(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	if got["hostname"] != "example" {
		t.Fatalf("expected hostname %q, got %q", "example", got["hostname"])
	}
	if got["port"] != "8080" {
		t.Fatalf("expected port %q, got %q", "8080", got["port"])
	}
}

func TestSystemControllerInstallFullLifecycle(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Start empty.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled (initial): %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed initially, got %d", len(pkgs.Entries))
	}

	// Install with responses.
	responses := packages.Responses{"hostname": "webserver", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	// Verify installed.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}

	// Verify responses.
	got, err := c.GetResponses(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if got["hostname"] != "webserver" {
		t.Fatalf("expected hostname %q, got %q", "webserver", got["hostname"])
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "2.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify uninstalled.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Verify responses empty after uninstall.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses after uninstall: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty responses after uninstall, got %v", resp)
	}
}

func TestSystemControllerInstallMultiplePackages(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}

	// Each package has its own responses.
	nginxResp, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses nginx@1.0: %v", err)
	}
	if nginxResp["hostname"] != "alpha" {
		t.Fatalf("expected nginx hostname %q, got %q", "alpha", nginxResp["hostname"])
	}

	redisResp, err := c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses redis@7.0: %v", err)
	}
	if redisResp["password"] != "secret" {
		t.Fatalf("expected redis password %q, got %q", "secret", redisResp["password"])
	}
}

// --- Install + Systemd integration tests ---

func TestSystemControllerInstallCreatesSystemdUnit(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// nginx has 1 external port (8080->80):
	//   3 InstallUnit (service, socket, networkcontroller) +
	//   2 Enable (socket, networkcontroller) + 1 Start(NC) + 1 Start(service) = 7
	calls := sd.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 systemd calls, got %d", len(calls))
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitArg, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitArg != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("call 0: expected unit %q, got %v", "town-os-package--core-nginx-1.0.service", calls[0].Args[0])
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	if lastCall.Method != "SetStatus" {
		t.Fatalf("last call: expected SetStatus, got %q", lastCall.Method)
	}
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastAction != systemd.Start {
		t.Fatalf("last call: expected Start, got %v", lastCall.Args[1])
	}
}

func TestSystemControllerUninstallRemovesSystemdUnit(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Install (7) + Uninstall: ListPackageUnitFiles + 3 units * (Stop+Disable+Uninstall) = 10 → total 17
	// (3 units: service, socket, networkcontroller)
	calls := sd.GetCalls()
	if len(calls) != 17 {
		t.Fatalf("expected 17 systemd calls, got %d", len(calls))
	}

	// Install phase: first call is InstallUnit for service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}

	// Install phase: last install call (index 6) is Start(service), index 5 is Start(NC).
	call6Action, ok := calls[6].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if call6Action != systemd.Start {
		t.Fatalf("call 6: expected Start, got %v", calls[6].Args[1])
	}

	// Uninstall phase starts at index 7: ListPackageUnitFiles, then Stop, Disable, UninstallUnit per unit.
	if calls[7].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 7: expected ListPackageUnitFiles, got %q", calls[7].Method)
	}
	if calls[8].Method != "SetStatus" {
		t.Fatalf("call 8: expected SetStatus, got %q", calls[8].Method)
	}
	stopAction, ok := calls[8].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if stopAction != systemd.Stop {
		t.Fatalf("call 8: expected Stop, got %v", calls[8].Args[1])
	}
}

func TestSystemControllerInstallUninstallFullLifecycle(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify listed as installed
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs.Entries[0])
	}

	// Verify 7 systemd calls from install (nginx has 1 ext port).
	calls := sd.GetCalls()
	if len(calls) != 7 {
		t.Fatalf("expected 7 systemd calls after install, got %d", len(calls))
	}

	// Uninstall
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Verify uninstalled
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Install (7) + Uninstall: ListPackageUnitFiles + 3 units * 3 ops = 10 → total 17
	// (3 units: service, socket, networkcontroller)
	calls = sd.GetCalls()
	if len(calls) != 17 {
		t.Fatalf("expected 17 systemd calls total, got %d", len(calls))
	}
}

func TestSystemControllerInstallMultiplePackagesSystemdUnits(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// nginx (1 ext port): 3 InstallUnit + 2 Enable + 1 Start(NC) + 1 Start(svc) = 7
	// redis (1 int port): 3 InstallUnit + 2 Enable + 1 Start(NC) + 1 Start(svc) = 7
	// Total = 14
	calls := sd.GetCalls()
	if len(calls) != 14 {
		t.Fatalf("expected 14 systemd calls, got %d", len(calls))
	}

	// First call is InstallUnit for nginx service.
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitArg, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitArg != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("call 0: expected unit %q, got %q", "town-os-package--core-nginx-1.0.service", calls[0].Args[0])
	}

	// Redis service starts at index 7 (nginx install is 7 calls: 3 Install + 2 Enable + 1 Start(NC) + 1 Start(svc)).
	if calls[7].Method != "InstallUnit" {
		t.Fatalf("call 7: expected InstallUnit, got %q", calls[7].Method)
	}
	unitArg2, ok := calls[7].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitArg2 != "town-os-package--core-redis-7.0.service" {
		t.Fatalf("call 7: expected unit %q, got %q", "town-os-package--core-redis-7.0.service", calls[7].Args[0])
	}
}

// TestSystemControllerUpgradeRemovesOldRecord verifies that upgrading from
// one version to another removes the old install record, leaving only the
// new version installed.
func TestSystemControllerUpgradeRemovesOldRecord(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install 1.0: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected [core/nginx@1.0], got %v", pkgs.Entries)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@2.0 (upgrade): %v", err)
	}

	// Verify only 2.0 remains installed.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after upgrade: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed after upgrade, got %d: %v", len(pkgs.Entries), pkgs.Entries)
	}
	if pkgs.Entries[0] != "core/nginx@2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s", pkgs.Entries[0])
	}

	// Verify responses are accessible for the new version.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetResponses nginx@2.0: %v", err)
	}
	if resp["hostname"] != "alpha" {
		t.Fatalf("expected hostname %q, got %q", "alpha", resp["hostname"])
	}
}

func TestInstallPackageWithGitSeed(t *testing.T) {
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

	// Create a local bare git repo to use as the seed source.
	seedRepo := filepath.Join(t.TempDir(), "seed.git")
	for _, args := range [][]string{
		{"init", "--bare", "-b", "main", seedRepo},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	// Clone, add a file, push to seed.
	workDir := filepath.Join(t.TempDir(), "work")
	for _, args := range [][]string{
		{"clone", seedRepo, workDir},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello from seed"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"-C", workDir, "add", "hello.txt"},
		{"-C", workDir, "-c", "user.name=test", "-c", "user.email=test@test", "-c", "commit.gpgsign=false", "commit", "-m", "seed"},
		{"-C", workDir, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Ensure bare repo HEAD points to main (default may be master on some systems).
	{
		cmd := exec.CommandContext(context.TODO(), "git", "-C", seedRepo, "symbolic-ref", "HEAD", "refs/heads/main")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git symbolic-ref HEAD (seed): %v: %s", err, out)
		}
	}

	// BtrfsBasePath points to a real temp dir so git clone can write files.
	btrfsBase := t.TempDir()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Create a local file-based package repository as a bare git repo so go-git
	// can clone it. We build the content in a working copy, push to a bare repo,
	// then point AddRepository at the bare repo URL.
	seedURL := "file://" + seedRepo
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
	pkgDir := filepath.Join(localWork, packages.PackagesDir, "myapp")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	pkgYAML := fmt.Sprintf("image: nginx:1.0\nvolumes:\n  config:\n    mountpoint: /config\n    git: %s\n", seedURL)
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"-C", localWork, "add", "."},
		{"-C", localWork, "-c", "user.name=test", "-c", "user.email=test@test", "commit", "-m", "init"},
		{"-C", localWork, "push", "origin", "HEAD:main"},
	} {
		cmd := exec.CommandContext(context.TODO(), "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Ensure bare repo HEAD points to main (default may be master on some systems).
	cmd := exec.CommandContext(context.TODO(), "git", "-C", localBareRepo, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v: %s", err, out)
	}

	if err := c.AddRepository(context.TODO(), "local", "file://"+localBareRepo, "", ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	// Pre-create the volume directory (mock storage doesn't create real dirs).
	volDir := filepath.Join(btrfsBase, "installed", "local", "myapp", "1.0", "config")
	if err := os.MkdirAll(volDir, 0750); err != nil {
		t.Fatalf("MkdirAll volume dir: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "myapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify git cloned files exist in the volume.
	helloPath := filepath.Join(volDir, "hello.txt")
	content, err := os.ReadFile(helloPath)
	if err != nil {
		t.Fatalf("expected hello.txt in git seed volume: %v", err)
	}
	if string(content) != "hello from seed" {
		t.Fatalf("expected 'hello from seed', got %q", string(content))
	}
}

func TestGitSourceInstallAndRebuild(t *testing.T) {
	t.Parallel()
	// Create a local bare git repo to serve as the git source.
	bareDir := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.CommandContext(context.TODO(), "git", "init", "--bare", "--initial-branch=main", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	// Clone the bare repo, add a file, and push.
	workDir := filepath.Join(t.TempDir(), "work")
	if out, err := exec.CommandContext(context.TODO(), "git", "clone", bareDir, workDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}

	testContent := "hello from git source"
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), []byte(testContent), 0644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}

	for _, args := range [][]string{
		{"git", "-C", workDir, "config", "user.email", "test@test.com"},
		{"git", "-C", workDir, "config", "user.name", "Test"},
		{"git", "-C", workDir, "config", "commit.gpgsign", "false"},
		{"git", "-C", workDir, "add", "."},
		{"git", "-C", workDir, "commit", "-m", "initial"},
		{"git", "-C", workDir, "push", "origin", "main"},
	} {
		if out, err := exec.CommandContext(context.TODO(), args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Set up the system controller with BtrfsBasePath and a package referencing the local git repo.
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	pkgYAML := fmt.Sprintf(`image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  site:
    mountpoint: /var/www/html
    git: file://%s
questions: {}
git_sources:
  - url: %s
    branch: main
    volume: site
`, bareDir, bareDir)

	repoName := "test-repo"
	pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, "mysite")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0644); err != nil {
		t.Fatalf("WriteFile package yaml: %v", err)
	}

	repoURL, err := url.Parse("https://example.com/test-repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if err := rr.Add(packages.Repository{Name: repoName, URL: *repoURL}); err != nil {
		t.Fatalf("Add repo: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		BtrfsBasePath:  btrfsBase,
		Git:            &git.GoGitClient{},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	// Pre-create the volume directory (mock storage doesn't create real dirs).
	volPath := fmt.Sprintf("installed/%s/mysite/1.0/site", repoName)
	volDir := filepath.Join(btrfsBase, volPath)
	if err := os.MkdirAll(volDir, 0750); err != nil {
		t.Fatalf("MkdirAll volume dir: %v", err)
	}

	// Install the package.
	if err := c.InstallPackage(context.TODO(), "mysite", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify the cloned file exists in the volume.
	clonedFile := filepath.Join(btrfsBase, volPath, "index.html")
	content, err := os.ReadFile(clonedFile)
	if err != nil {
		t.Fatalf("ReadFile after clone: %v", err)
	}
	if string(content) != testContent {
		t.Fatalf("expected %q, got %q", testContent, string(content))
	}

	// Push a new commit to the bare repo.
	updatedContent := "updated content"
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), []byte(updatedContent), 0644); err != nil {
		t.Fatalf("WriteFile updated: %v", err)
	}
	for _, args := range [][]string{
		{"git", "-C", workDir, "add", "."},
		{"git", "-C", workDir, "commit", "-m", "update"},
		{"git", "-C", workDir, "push", "origin", "main"},
	} {
		if out, err := exec.CommandContext(context.TODO(), args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Call rebuild-git.
	if err := c.RebuildGitSources(context.TODO(), repoName, "mysite", "1.0"); err != nil {
		t.Fatalf("RebuildGitSources: %v", err)
	}

	// Verify the updated file content.
	content, err = os.ReadFile(clonedFile)
	if err != nil {
		t.Fatalf("ReadFile after rebuild: %v", err)
	}
	if string(content) != updatedContent {
		t.Fatalf("expected %q after rebuild, got %q", updatedContent, string(content))
	}
}

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
