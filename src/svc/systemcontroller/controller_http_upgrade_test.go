// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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

func TestHTTPUpgradeMovesVolumesAndCleansOldRecord(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	volYAML := func(version string) string {
		return fmt.Sprintf(`image: nginx:%s
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`, version)
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", volYAML("1.0"))
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", volYAML("2.0"))

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Upgrade to nginx 2.0 — should auto-move volumes and remove old record.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Only version 2.0 should be in the installed list.
	pkgs, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if !strings.Contains(pkgs.Entries[0], "2.0") {
		t.Fatalf("expected version 2.0 in entry, got %s", pkgs.Entries[0])
	}

	// The data volume should have been renamed from 1.0 path to 2.0 path.
	calls := mockCtrl.GetLog()
	foundRename := false
	for _, call := range calls {
		if call.Operation == "SubvolRename" && len(call.Arguments) == 2 {
			if call.Arguments[0] == "installed/repo-a/nginx/1.0/data" && call.Arguments[1] == "installed/repo-a/nginx/2.0/data" {
				foundRename = true
			}
		}
	}
	if !foundRename {
		t.Fatal("expected SubvolRename call to move data volume from 1.0 to 2.0 path")
	}
}

func TestHTTPUpgradeStopsOldUnit(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	nginx20 := `image: nginx:2.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	calls := sd.GetCalls()
	// First install: InstallUnit + Start = 2
	// Upgrade: ListPackageUnitFiles + Stop + Disable + UninstallUnit + InstallUnit + Start = 6
	// Total = 8
	if len(calls) != 8 {
		methods := make([]string, len(calls))
		for i, cl := range calls {
			methods[i] = cl.Method
		}
		t.Fatalf("expected 8 systemd calls, got %d: %v", len(calls), methods)
	}

	// First install: InstallUnit, Start
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	upgradeFirstStart, ok := calls[1].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if upgradeFirstStart != systemd.Start {
		t.Fatalf("call 1: expected Start, got %v", calls[1].Args[1])
	}

	// Upgrade teardown: ListPackageUnitFiles, Stop, Disable, UninstallUnit
	if calls[2].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 2: expected ListPackageUnitFiles, got %q", calls[2].Method)
	}
	upgradeStop, ok := calls[3].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if upgradeStop != systemd.Stop {
		t.Fatalf("call 3: expected Stop, got %v", calls[3].Args[1])
	}
	upgradeDisable, ok := calls[4].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if upgradeDisable != systemd.Disable {
		t.Fatalf("call 4: expected Disable, got %v", calls[4].Args[1])
	}
	if calls[5].Method != "UninstallUnit" {
		t.Fatalf("call 5: expected UninstallUnit, got %q", calls[5].Method)
	}

	// Upgrade setup: InstallUnit, Start
	if calls[6].Method != "InstallUnit" {
		t.Fatalf("call 6: expected InstallUnit, got %q", calls[6].Method)
	}
	upgradeNewStart, ok := calls[7].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if upgradeNewStart != systemd.Start {
		t.Fatalf("call 7: expected Start, got %v", calls[7].Args[1])
	}
}

func TestHTTPUpgradeAutoMovesVolumes(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 1gb
`
	nginx20 := `image: nginx:2.0
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 2gb
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Verify 1.0 volume was created.
	fs := mockCtrl.GetFilesystems()
	found10 := false
	for _, f := range fs {
		if f.Name == "installed/repo-a/nginx/1.0/data" {
			found10 = true
		}
	}
	if !found10 {
		t.Fatalf("expected installed/repo-a/nginx/1.0/data volume, filesystems: %v", fs)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Verify volume was moved (renamed) from 1.0 to 2.0.
	fs = mockCtrl.GetFilesystems()
	found20 := false
	still10 := false
	for _, f := range fs {
		if f.Name == "installed/repo-a/nginx/2.0/data" {
			found20 = true
		}
		if f.Name == "installed/repo-a/nginx/1.0/data" {
			still10 = true
		}
	}
	if !found20 {
		t.Fatalf("expected installed/repo-a/nginx/2.0/data volume after upgrade, filesystems: %v", fs)
	}
	if still10 {
		t.Fatalf("expected installed/repo-a/nginx/1.0/data to be moved, but it still exists")
	}

	// Verify SubvolRename was called with correct args.
	calls := mockCtrl.GetLog()
	foundRename := false
	for _, call := range calls {
		if call.Operation == "SubvolRename" {
			args := call.Arguments
			if len(args) >= 2 {
				src, ok := args[0].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				dst, ok := args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				if src == "installed/repo-a/nginx/1.0/data" && dst == "installed/repo-a/nginx/2.0/data" {
					foundRename = true
				}
			}
		}
	}
	if !foundRename {
		t.Fatal("expected SubvolRename call to move volume from 1.0 to 2.0")
	}
}

func TestHTTPUpgradeRemovesOldInstallRecord(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Verify 1.0 is installed.
	installed, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(installed.Entries) != 1 || installed.Entries[0] != "repo-a/nginx@1.0" {
		t.Fatalf("expected [repo-a/nginx@1.0], got %v", installed.Entries)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Verify only 2.0 is installed (old record removed).
	installed, err = c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(installed.Entries) != 1 {
		t.Fatalf("expected 1 installed package after upgrade, got %d: %v", len(installed.Entries), installed.Entries)
	}
	if installed.Entries[0] != "repo-a/nginx@2.0" {
		t.Fatalf("expected repo-a/nginx@2.0, got %s", installed.Entries[0])
	}
}

func TestHTTPUpgradeNewVolumesCreatedFresh(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 1gb
`
	nginx20 := `image: nginx:2.0
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 2gb
  cache:
    mountpoint: /var/cache/nginx
    quota: 500mb
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Upgrade to nginx 2.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Verify both volumes exist for version 2.0.
	fs := mockCtrl.GetFilesystems()
	foundData := false
	foundCache := false
	for _, f := range fs {
		if f.Name == "installed/repo-a/nginx/2.0/data" {
			foundData = true
		}
		if f.Name == "installed/repo-a/nginx/2.0/cache" {
			foundCache = true
		}
	}
	if !foundData {
		t.Fatalf("expected installed/repo-a/nginx/2.0/data volume, filesystems: %v", fs)
	}
	if !foundCache {
		t.Fatalf("expected installed/repo-a/nginx/2.0/cache volume (new in v2), filesystems: %v", fs)
	}

	// Verify "data" was renamed (moved from 1.0) and "cache" was created fresh.
	calls := mockCtrl.GetLog()
	foundRename := false
	foundCreate := false
	for _, call := range calls {
		if call.Operation == "SubvolRename" {
			args := call.Arguments
			if len(args) >= 2 {
				src, ok := args[0].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				dst, ok := args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				if src == "installed/repo-a/nginx/1.0/data" && dst == "installed/repo-a/nginx/2.0/data" {
					foundRename = true
				}
			}
		}
		if call.Operation == "SubvolCreate" {
			if len(call.Arguments) > 0 {
				name, ok := call.Arguments[0].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				if name == "installed/repo-a/nginx/2.0/cache" {
					foundCreate = true
				}
			}
		}
	}
	if !foundRename {
		t.Fatal("expected SubvolRename for data volume from 1.0 to 2.0")
	}
	if !foundCreate {
		t.Fatal("expected SubvolCreate for new cache volume in 2.0")
	}
}

func TestHTTPUpgradeExplicitImportOverridesAuto(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx10 := `image: nginx:1.0
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 1gb
`
	nginx20 := `image: nginx:2.0
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 2gb
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install nginx 1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Upgrade to 2.0 with explicit ImportFromVersion (should use snapshot, not rename).
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "1.0", false); err != nil {
		t.Fatalf("InstallPackage 2.0 with import: %v", err)
	}

	// Verify snapshot was used instead of rename.
	calls := mockCtrl.GetLog()
	foundSnapshot := false
	foundRename := false
	for _, call := range calls {
		if call.Operation == "SubvolSnapshot" {
			foundSnapshot = true
		}
		if call.Operation == "SubvolRename" {
			args := call.Arguments
			if len(args) >= 2 {
				src, ok := args[0].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				if src == "installed/repo-a/nginx/1.0/data" {
					foundRename = true
				}
			}
		}
	}
	if !foundSnapshot {
		t.Fatal("expected SubvolSnapshot when ImportFromVersion is explicit")
	}
	if foundRename {
		t.Fatal("expected no SubvolRename when ImportFromVersion is explicitly set")
	}
}

func TestHTTPListUpgradesShowsAvailable(t *testing.T) {
	c, _ := initUpgradesTestServer(t)

	upgrades, err := c.ListUpgrades(context.TODO())
	if err != nil {
		t.Fatalf("ListUpgrades: %v", err)
	}

	if len(upgrades) != 1 {
		t.Fatalf("expected 1 upgrade, got %d", len(upgrades))
	}

	u := upgrades[0]
	if u.Name != "nginx" {
		t.Fatalf("expected nginx, got %s", u.Name)
	}
	if u.InstalledVersion != "1.0" {
		t.Fatalf("expected installed 1.0, got %s", u.InstalledVersion)
	}
	if u.LatestVersion != "2.0" {
		t.Fatalf("expected latest 2.0, got %s", u.LatestVersion)
	}
}

func TestHTTPListUpgradesEmpty(t *testing.T) {
	dir := t.TempDir()
	repoName := "test-repo"
	pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repos := []packages.Repository{{Name: repoName, URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}}}
	repoData, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("json.Marshal repos: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), repoData, 0600); err != nil {
		t.Fatalf("WriteFile repos: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	if err := inst.Install(repoName, "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	ts := InitTestServer(ServerConfig{RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	upgrades, err := c.ListUpgrades(context.TODO())
	if err != nil {
		t.Fatalf("ListUpgrades: %v", err)
	}

	if len(upgrades) != 0 {
		t.Fatalf("expected 0 upgrades, got %d", len(upgrades))
	}
}

func TestHTTPDismissUpgrades(t *testing.T) {
	c, _ := initUpgradesTestServer(t)

	if err := c.DismissUpgrades(context.TODO()); err != nil {
		t.Fatalf("DismissUpgrades: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if !ping.UpgradesDismissed {
		t.Fatal("expected upgrades to be dismissed after DismissUpgrades")
	}
}

func TestHTTPUpgradesChangedFlag(t *testing.T) {
	c, inst := initUpgradesTestServer(t)

	// Break the hard link by removing and recreating the repo file.
	repoFile := filepath.Join(inst.BaseDir, "test-repo", packages.PackagesDir, "nginx", "1.0.yaml")
	if err := os.Remove(repoFile); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0-updated\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	upgrades, err := c.ListUpgrades(context.TODO())
	if err != nil {
		t.Fatalf("ListUpgrades: %v", err)
	}

	if len(upgrades) != 1 {
		t.Fatalf("expected 1 upgrade, got %d", len(upgrades))
	}

	if !upgrades[0].Changed {
		t.Fatal("expected Changed=true after repo file was modified")
	}
}
