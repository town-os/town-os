package systemcontroller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPInstallPackage(t *testing.T) {
	c, inst := initInstallTestClient(t)

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}
	if calls[0].Method != "ListInstalled" {
		t.Fatalf("expected ListInstalled call, got %q", calls[0].Method)
	}
	if calls[1].Method != "ListInstalled" {
		t.Fatalf("expected ListInstalled call (port exclusion), got %q", calls[1].Method)
	}
	if calls[2].Method != "Install" {
		t.Fatalf("expected Install call, got %q", calls[2].Method)
	}
	repoArg, ok := calls[2].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if repoArg != "repo-a" {
		t.Fatalf("expected repoName %q, got %v", "repo-a", calls[2].Args[0])
	}
	nameArg, ok := calls[2].Args[1].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if nameArg != "nginx" {
		t.Fatalf("expected pkgName %q, got %v", "nginx", calls[2].Args[1])
	}
	verArg, ok := calls[2].Args[2].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if verArg != "1.0" {
		t.Fatalf("expected version %q, got %v", "1.0", calls[2].Args[2])
	}
	if calls[3].Method != "ClearLastResponses" {
		t.Fatalf("expected ClearLastResponses call, got %q", calls[3].Method)
	}
}

func TestHTTPInstallPackageAutoGeneratesSecret(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: myapp:1.0
environment:
  DB_PASSWORD: "@dbpass@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  dbpass:
    query: "Database password:"
    type: secret
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "myapp", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install with empty responses — secret should be auto-generated.
	if err := c.InstallPackage(context.TODO(), "myapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	var installCall *packages.MockInstallCall
	for i := range calls {
		if calls[i].Method == "Install" {
			installCall = &calls[i]
			break
		}
	}
	if installCall == nil {
		t.Fatal("expected an Install call")
	}

	resp, ok := installCall.Args[3].(packages.Responses)
	if !ok {
		t.Fatal("type assertion failed")
	}
	secret := resp["dbpass"]
	if secret == "" {
		t.Fatal("expected auto-generated secret, got empty string")
	}
	if len(secret) != 64 {
		t.Fatalf("expected 64-char hex secret, got %d chars: %q", len(secret), secret)
	}
}

func TestHTTPInstallPackageSecretUserOverride(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: myapp:1.0
environment:
  DB_PASSWORD: "@dbpass@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  dbpass:
    query: "Database password:"
    type: secret
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "myapp", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install with explicit secret — should pass through unchanged.
	userSecret := "my-custom-password-value"
	if err := c.InstallPackage(context.TODO(), "myapp", "1.0", packages.Responses{"dbpass": userSecret}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	var installCall *packages.MockInstallCall
	for i := range calls {
		if calls[i].Method == "Install" {
			installCall = &calls[i]
			break
		}
	}
	if installCall == nil {
		t.Fatal("expected an Install call")
	}

	resp, ok := installCall.Args[3].(packages.Responses)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if resp["dbpass"] != userSecret {
		t.Fatalf("expected user-provided secret %q, got %q", userSecret, resp["dbpass"])
	}
}

func TestHTTPInstallPackageNotFound(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.InstallPackage(context.TODO(),"nonexistent", "1.0", packages.Responses{}, false, "", false)
	if err == nil {
		t.Fatal("expected error installing nonexistent package")
	}
}

func TestHTTPInstallPackageBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages/install"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPUninstallPackage(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Install first so uninstall can succeed.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := inst.GetCalls()
	// Install phase: ListInstalled + ListInstalled + Install + ClearLastResponses = 4
	// Uninstall phase: GetResponses + SaveLastResponses + SetDisabled + Uninstall + ListInstalled = 5
	// Total = 9
	if len(calls) != 9 {
		t.Fatalf("expected 9 calls, got %d: %v", len(calls), calls)
	}
	if calls[2].Method != "Install" {
		t.Fatalf("expected Install call, got %q", calls[2].Method)
	}
	if calls[6].Method != "SetDisabled" {
		t.Fatalf("expected SetDisabled call, got %q", calls[6].Method)
	}
	if calls[7].Method != "Uninstall" {
		t.Fatalf("expected Uninstall call, got %q", calls[7].Method)
	}
	if calls[8].Method != "ListInstalled" {
		t.Fatalf("expected ListInstalled call, got %q", calls[8].Method)
	}
}

func TestHTTPUninstallPackageNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false)
	if err == nil {
		t.Fatal("expected error uninstalling package that is not installed")
	}
}

func TestHTTPUninstallPackageWithPurge(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were NOT created (nginx 1.0 has no volumes in the test fixture).
	// Install with purge=true should still succeed even with no volumes.
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage with purge: %v", err)
	}
}

func TestHTTPUninstallPackagePurgesVolumes(t *testing.T) {
	c, controller := initInstallWithVolumesTestClient(t)

	// Install a package that defines volumes.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were created.
	before := controller.GetFilesystems()
	volNames := map[string]bool{}
	for _, fs := range before {
		volNames[fs.Name] = true
	}
	if !volNames["installed/repo-a/nginx/1.0/html"] {
		t.Fatal("expected installed/repo-a/nginx/1.0/html volume to exist after install")
	}
	if !volNames["installed/repo-a/nginx/1.0/logs"] {
		t.Fatal("expected installed/repo-a/nginx/1.0/logs volume to exist after install")
	}

	// Uninstall with purge.
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage with purge: %v", err)
	}

	// Verify all nginx volumes are gone.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/repo-a/nginx" || strings.HasPrefix(fs.Name, "installed/repo-a/nginx/") {
			t.Fatalf("expected all nginx volumes purged, found %q", fs.Name)
		}
	}
}

func TestHTTPUninstallPackageWithoutPurgePreservesVolumes(t *testing.T) {
	c, controller := initInstallWithVolumesTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Uninstall WITHOUT purge.
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify volumes are renamed to uninstalled/.
	after := controller.GetFilesystems()
	volNames := map[string]bool{}
	for _, fs := range after {
		volNames[fs.Name] = true
	}
	if !volNames["uninstalled/repo-a/nginx/1.0/html"] {
		t.Fatal("expected uninstalled/repo-a/nginx/1.0/html volume preserved after uninstall without purge")
	}
	if !volNames["uninstalled/repo-a/nginx/1.0/logs"] {
		t.Fatal("expected uninstalled/repo-a/nginx/1.0/logs volume preserved after uninstall without purge")
	}
}

func TestHTTPPurgeVolumes(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject package volume entries directly into the mock controller.
	injectSubvol(t, controller, "installed/mock-repo/nginx/1.0/html", 1024)
	injectSubvol(t, controller, "installed/mock-repo/nginx/1.0/logs", 2048)
	injectSubvol(t, controller, "installed/mock-repo/other/1.0/data", 512)

	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	remaining := controller.GetFilesystems()
	for _, fs := range remaining {
		if fs.Name == "installed/mock-repo/nginx" || strings.HasPrefix(fs.Name, "installed/mock-repo/nginx/") {
			t.Fatalf("expected nginx volumes to be purged, found %s", fs.Name)
		}
	}

	found := false
	for _, fs := range remaining {
		if fs.Name == "installed/mock-repo/other/1.0/data" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected installed/mock-repo/other/1.0/data volume to be preserved")
	}
}

func TestHTTPPurgeVolumesVerifiesControllerState(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject package volume hierarchy directly into the mock controller.
	for _, name := range []string{"installed/mock-repo/nginx/1.0/html", "installed/mock-repo/nginx/1.0/logs", "installed/mock-repo/nginx/1.0/cache/tmp"} {
		injectSubvol(t, controller, name, 0)
	}

	// Also create a volume for a different package.
	injectSubvol(t, controller, "installed/mock-repo/redis/7.0/data", 0)

	// Verify volumes exist before purge.
	before := controller.GetFilesystems()
	expectedBefore := map[string]bool{
		"installed":                                true,
		"installed/mock-repo":                      true,
		"installed/mock-repo/nginx":                true,
		"installed/mock-repo/nginx/1.0":            true,
		"installed/mock-repo/nginx/1.0/html":       true,
		"installed/mock-repo/nginx/1.0/logs":       true,
		"installed/mock-repo/nginx/1.0/cache":      true,
		"installed/mock-repo/nginx/1.0/cache/tmp":  true,
		"installed/mock-repo/redis":                true,
		"installed/mock-repo/redis/7.0":            true,
		"installed/mock-repo/redis/7.0/data":       true,
	}
	for _, fs := range before {
		if !expectedBefore[fs.Name] {
			t.Fatalf("unexpected filesystem before purge: %q", fs.Name)
		}
		delete(expectedBefore, fs.Name)
	}
	if len(expectedBefore) > 0 {
		t.Fatalf("missing filesystems before purge: %v", expectedBefore)
	}

	// Purge nginx.
	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify at the controller level that nginx volumes are gone.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/mock-repo/nginx" || strings.HasPrefix(fs.Name, "installed/mock-repo/nginx/") {
			t.Fatalf("expected all nginx volumes purged, found %q in controller", fs.Name)
		}
	}

	// Verify redis volumes are untouched.
	redisFound := map[string]bool{}
	for _, fs := range after {
		redisFound[fs.Name] = true
	}
	if !redisFound["installed/mock-repo/redis"] {
		t.Fatal("expected installed/mock-repo/redis parent volume to survive purge")
	}
	if !redisFound["installed/mock-repo/redis/7.0/data"] {
		t.Fatal("expected installed/mock-repo/redis/7.0/data volume to survive purge")
	}
}

func TestHTTPPurgeVolumesSimilarPrefix(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject volumes with similar prefixes: nginx and nginx2 are separate packages.
	for _, name := range []string{"installed/mock-repo/nginx/1.0/html", "installed/mock-repo/nginx2/1.0/data"} {
		injectSubvol(t, controller, name, 0)
	}

	// Purge only nginx.
	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify nginx is gone, nginx2 is intact.
	after := controller.GetFilesystems()
	found := map[string]bool{}
	for _, fs := range after {
		found[fs.Name] = true
	}

	if found["installed/mock-repo/nginx"] || found["installed/mock-repo/nginx/1.0/html"] {
		t.Fatal("expected nginx volumes to be purged")
	}
	if !found["installed/mock-repo/nginx2"] {
		t.Fatal("expected installed/mock-repo/nginx2 parent to survive")
	}
	if !found["installed/mock-repo/nginx2/1.0/data"] {
		t.Fatal("expected installed/mock-repo/nginx2/1.0/data to survive")
	}
}

func TestHTTPPurgeVolumesDeepNesting(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject deeply nested volumes: installed/mock-repo/pkg/1.0/a/b/c/d.
	injectSubvol(t, controller, "installed/mock-repo/pkg/1.0/a/b/c/d", 0)

	// Purge the package.
	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "pkg"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Everything under installed/mock-repo/pkg should be gone; only the installed root remains.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/mock-repo/pkg" || strings.HasPrefix(fs.Name, "installed/mock-repo/pkg/") {
			t.Fatalf("expected all pkg volumes purged, found %q", fs.Name)
		}
	}
}

func TestHTTPPurgeVolumesEmpty(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject a volume for a different package.
	injectSubvol(t, controller, "installed/mock-repo/redis/7.0/data", 0)

	// Purge a package that has no volumes.
	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "nginx"); err != nil {
		t.Fatalf("PurgeVolumes should succeed for nonexistent package: %v", err)
	}

	// Redis volumes must be untouched.
	after := controller.GetFilesystems()
	found := map[string]bool{}
	for _, fs := range after {
		found[fs.Name] = true
	}
	if !found["installed/mock-repo/redis"] || !found["installed/mock-repo/redis/7.0/data"] {
		t.Fatalf("expected redis volumes to be intact, got: %v", after)
	}
}

func TestHTTPPurgeVolumesWithQuotas(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject volumes with quotas.
	injectSubvol(t, controller, "installed/mock-repo/nginx/1.0/html", 1024)
	injectSubvol(t, controller, "installed/mock-repo/nginx/1.0/logs", 2048)

	// Purge.
	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "nginx"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify all nginx volumes gone from controller.
	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/mock-repo/nginx" || strings.HasPrefix(fs.Name, "installed/mock-repo/nginx/") {
			t.Fatalf("expected all nginx volumes purged, found %q", fs.Name)
		}
	}

	// Verify quotas are cleaned up too.
	for k := range controller.Quotas {
		if strings.HasPrefix(k, "installed/mock-repo/nginx/") {
			t.Fatalf("expected nginx quotas cleaned up, found quota for %q", k)
		}
	}
}

func TestHTTPPurgeVolumesMultipleChildren(t *testing.T) {
	c, controller := initTestClient(t)

	// Inject many children under a single parent.
	children := []string{
		"installed/mock-repo/app/1.0/data", "installed/mock-repo/app/1.0/logs", "installed/mock-repo/app/1.0/cache",
		"installed/mock-repo/app/1.0/tmp", "installed/mock-repo/app/1.0/config",
	}
	for _, name := range children {
		injectSubvol(t, controller, name, 0)
	}

	if err := c.PurgeVolumes(context.TODO(), "mock-repo", "app"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	after := controller.GetFilesystems()
	for _, fs := range after {
		if fs.Name == "installed/mock-repo/app" || strings.HasPrefix(fs.Name, "installed/mock-repo/app/") {
			t.Fatalf("expected all app volumes purged, found %q", fs.Name)
		}
	}
}

func TestHTTPUninstallPackageBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages/uninstall"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPInstallPackageCreatesSystemdUnit(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 systemd calls, got %d: %v", len(calls), calls)
	}

	// 1. InstallUnit
	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitName != "town-os-package--repo-a-nginx-1.0.service" {
		t.Fatalf("call 0: expected unit name %q, got %v", "town-os-package--repo-a-nginx-1.0.service", calls[0].Args[0])
	}

	// 2. SetStatus(start)
	if calls[1].Method != "SetStatus" {
		t.Fatalf("call 1: expected SetStatus, got %q", calls[1].Method)
	}
	startAction, ok := calls[1].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if startAction != systemd.Start {
		t.Fatalf("call 1: expected action %q, got %v", systemd.Start, calls[1].Args[1])
	}
}

func TestHTTPUninstallPackageRemovesSystemdUnit(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	// Install first so uninstall can succeed.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	calls := sd.GetCalls()
	// Install produces 2 calls (InstallUnit, Start)
	// Uninstall produces 4 calls (ListPackageUnitFiles, Stop, Disable, UninstallUnit)
	if len(calls) != 6 {
		methods := make([]string, len(calls))
		for i, cl := range calls {
			methods[i] = cl.Method
		}
		t.Fatalf("expected 6 systemd calls, got %d: %v", len(calls), methods)
	}

	// Uninstall calls: indices 2, 3, 4, 5
	// 2. ListPackageUnitFiles
	if calls[2].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 2: expected ListPackageUnitFiles, got %q", calls[2].Method)
	}

	// 3. SetStatus(Stop)
	if calls[3].Method != "SetStatus" {
		t.Fatalf("call 3: expected SetStatus, got %q", calls[3].Method)
	}
	stopAction, ok := calls[3].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if stopAction != systemd.Stop {
		t.Fatalf("call 3: expected action %q, got %v", systemd.Stop, calls[3].Args[1])
	}

	// 4. SetStatus(Disable)
	if calls[4].Method != "SetStatus" {
		t.Fatalf("call 4: expected SetStatus, got %q", calls[4].Method)
	}
	disableAction, ok := calls[4].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if disableAction != systemd.Disable {
		t.Fatalf("call 4: expected action %q, got %v", systemd.Disable, calls[4].Args[1])
	}

	// 5. UninstallUnit
	if calls[5].Method != "UninstallUnit" {
		t.Fatalf("call 5: expected UninstallUnit, got %q", calls[5].Method)
	}
	uninstallUnitName, ok := calls[5].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if uninstallUnitName != "town-os-package--repo-a-nginx-1.0.service" {
		t.Fatalf("call 5: expected unit name %q, got %v", "town-os-package--repo-a-nginx-1.0.service", calls[5].Args[0])
	}
}

// --- Install creates storage volumes ---

func TestHTTPInstallPackageCreatesVolumes(t *testing.T) {
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

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 1gb
  logs:
    mountpoint: /var/log/app
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "myapp", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"myapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were created under installed/<repo>/<name>/<version>/.
	fs := mockCtrl.GetFilesystems()

	foundData := false
	foundLogs := false
	for _, f := range fs {
		if f.Name == "installed/repo-a/myapp/1.0/data" {
			foundData = true
		}
		if f.Name == "installed/repo-a/myapp/1.0/logs" {
			foundLogs = true
		}
	}

	if !foundData {
		t.Fatalf("expected filesystem installed/repo-a/myapp/1.0/data to be created, got: %v", fs)
	}
	if !foundLogs {
		t.Fatalf("expected filesystem installed/repo-a/myapp/1.0/logs to be created, got: %v", fs)
	}
}

func TestHTTPInstallPackageCreatesVolumesWithQuota(t *testing.T) {
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

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 2gb
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "myapp", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"myapp", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify quota was set on the volume.
	quota := mockCtrl.Quotas["installed/repo-a/myapp/1.0/data"]
	if quota != 2147483648 {
		t.Fatalf("expected quota 2147483648, got %d", quota)
	}
}

func TestHTTPInstallPackageNoVolumes(t *testing.T) {
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

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// No volumes to create — mock storage should be empty.
	fs := mockCtrl.GetFilesystems()
	if len(fs) != 0 {
		t.Fatalf("expected no filesystems, got %v", fs)
	}
}

func TestHTTPInstallPackageVolumesWithTemplatedQuota(t *testing.T) {
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

	pkgYAML := `image: postgres:16
environment: {}
network:
  external: {}
  internal: {}
volumes:
  pgdata:
    mountpoint: /var/lib/postgresql/data
    quota: "@size@"
questions:
  size:
    query: "How much storage for the database?"
    type: bytes
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "postgres", "16.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(),"postgres", "16.0", packages.Responses{"size": "10gb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify the volume was created with the templated quota.
	fs := mockCtrl.GetFilesystems()
	found := false
	for _, f := range fs {
		if f.Name == "installed/repo-a/postgres/16.0/pgdata" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected filesystem installed/repo-a/postgres/16.0/pgdata to be created, got: %v", fs)
	}

	// 10GB = 10 * 1024^3 = 10737418240
	quota := mockCtrl.Quotas["installed/repo-a/postgres/16.0/pgdata"]
	if quota != 10737418240 {
		t.Fatalf("expected quota 10737418240, got %d", quota)
	}
}

func TestHTTPListInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(),"nginx", "2.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@2.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	// Upgrade from 1.0 to 2.0 removes the old install record, so only 1 remains.
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if !strings.Contains(pkgs.Entries[0], "2.0") {
		t.Fatalf("expected version 2.0 in entry, got %s", pkgs.Entries[0])
	}
}

func TestHTTPListInstalledEmpty(t *testing.T) {
	c, _ := initInstallTestClient(t)

	pkgs, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(pkgs.Entries))
	}
}

func TestHTTPGetResponses(t *testing.T) {
	c, _ := initInstallTestClient(t)

	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	got, err := c.GetResponses(context.TODO(), "repo-a", "nginx", "1.0")
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

func TestHTTPGetResponsesNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	resp, err := c.GetResponses(context.TODO(), "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("expected empty responses for uninstalled package, got error: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 responses, got %d", len(resp))
	}
}

func TestHTTPGetResponsesBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages/responses"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPGetLastResponsesEmpty(t *testing.T) {
	c, _ := initInstallTestClient(t)

	resp, err := c.GetLastResponses(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("GetLastResponses: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 last responses, got %d", len(resp))
	}
}

func TestHTTPGetLastResponsesAfterUninstall(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Simulate saved last responses (as happens after uninstall).
	if err := inst.SaveLastResponses("repo-a", "nginx", packages.Responses{"hostname": "cached", "port": "9090"}); err != nil {
		t.Fatalf("SaveLastResponses: %v", err)
	}

	resp, err := c.GetLastResponses(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("GetLastResponses: %v", err)
	}

	if resp["hostname"] != "cached" {
		t.Fatalf("expected hostname %q, got %q", "cached", resp["hostname"])
	}
	if resp["port"] != "9090" {
		t.Fatalf("expected port %q, got %q", "9090", resp["port"])
	}
}

func TestHTTPClearLastResponses(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Save some last responses.
	if err := inst.SaveLastResponses("repo-a", "nginx", packages.Responses{"hostname": "cached"}); err != nil {
		t.Fatalf("SaveLastResponses: %v", err)
	}

	// Clear them via HTTP.
	if err := c.ClearLastResponses(context.TODO(), "repo-a", "nginx"); err != nil {
		t.Fatalf("ClearLastResponses: %v", err)
	}

	// Verify they are gone.
	resp, err := c.GetLastResponses(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("GetLastResponses after clear: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 last responses after clear, got %d", len(resp))
	}
}

func TestHTTPUninstallSavesLastResponsesWithCorrectData(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Install with specific responses.
	responses := packages.Responses{"hostname": "savetest", "port": "4444"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Uninstall to trigger SaveLastResponses.
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify SaveLastResponses was called with the correct responses.
	calls := inst.GetCalls()
	var saveCall *packages.MockInstallCall
	for i := range calls {
		if calls[i].Method == "SaveLastResponses" {
			saveCall = &calls[i]
			break
		}
	}
	if saveCall == nil {
		t.Fatal("expected SaveLastResponses call during uninstall")
	}

	savedResp, ok := saveCall.Args[2].(packages.Responses)
	if !ok {
		t.Fatal("type assertion failed for SaveLastResponses responses arg")
	}
	if savedResp["hostname"] != "savetest" {
		t.Fatalf("expected hostname %q in saved last responses, got %q", "savetest", savedResp["hostname"])
	}
	if savedResp["port"] != "4444" {
		t.Fatalf("expected port %q in saved last responses, got %q", "4444", savedResp["port"])
	}
}

func TestHTTPInstallAfterUninstallUsesLastResponses(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Install with responses, then uninstall (which saves last responses).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "original", "port": "5555"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify last responses are available after uninstall.
	lastResp, err := c.GetLastResponses(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("GetLastResponses: %v", err)
	}
	if lastResp["hostname"] != "original" {
		t.Fatalf("expected hostname %q, got %q", "original", lastResp["hostname"])
	}

	// Reinstall with new responses — should succeed and clear last responses.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "newhost", "port": "6666"}, false, "", false); err != nil {
		t.Fatalf("Reinstall: %v", err)
	}

	// Verify ClearLastResponses was called after reinstall.
	calls := inst.GetCalls()
	clearCount := 0
	for _, call := range calls {
		if call.Method == "ClearLastResponses" {
			clearCount++
		}
	}
	if clearCount < 2 {
		t.Fatalf("expected at least 2 ClearLastResponses calls (one per install), got %d", clearCount)
	}

	// Verify last responses are now empty.
	cleared, err := c.GetLastResponses(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("GetLastResponses after reinstall: %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected 0 last responses after reinstall, got %d", len(cleared))
	}
}

func TestHTTPGetLastResponsesBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages/last-responses"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPClearLastResponsesBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: emptyRepoRoot(t)})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages/clear-last-responses"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPGetInstalledInfo(t *testing.T) {
	c, _ := initInstallTestClient(t)

	// Install nginx with responses
	responses := packages.Responses{"hostname": "testhost", "port": "8081"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	info, err := c.GetInstalledInfo(context.TODO(), "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetInstalledInfo: %v", err)
	}

	// Verify questions
	if info.Questions["hostname"].Query != "What hostname should nginx serve?" {
		t.Fatalf("expected hostname query, got %q", info.Questions["hostname"].Query)
	}
	if info.Questions["port"].Query != "What external port should nginx listen on?" {
		t.Fatalf("expected port query, got %q", info.Questions["port"].Query)
	}

	// Verify responses
	if info.Responses["hostname"] != "testhost" {
		t.Fatalf("expected hostname=testhost, got %q", info.Responses["hostname"])
	}
	if info.Responses["port"] != "8081" {
		t.Fatalf("expected port=8081, got %q", info.Responses["port"])
	}

	// Verify compiled notes
	if info.Notes["URL"] != "http://testhost:8081" {
		t.Fatalf("expected URL=http://testhost:8081, got %q", info.Notes["URL"])
	}

	// Verify note types
	if info.NoteTypes["URL"] != packages.NoteURL {
		t.Fatalf("expected NoteTypes[URL]=%q, got %q", packages.NoteURL, info.NoteTypes["URL"])
	}
}

func TestHTTPGetInstalledInfoNotInstalled(t *testing.T) {
	c, _ := initInstallTestClient(t)

	_, err := c.GetInstalledInfo(context.TODO(), "repo-a", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error getting info for uninstalled package")
	}
}

// --- Install validation errors ---

func TestHTTPInstallValidationErrors(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// nginx@1.0 has questions: hostname (hostname type) and port (port type).
	// Empty responses are now auto-generated for hostname and port types,
	// so the install should succeed. Verify the auto-generated values were used.
	err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{}, false, "", false)
	if err != nil {
		t.Fatalf("expected install to succeed with auto-generated responses, got: %v", err)
	}

	calls := inst.GetCalls()
	// Find the Install call and verify responses were auto-generated.
	var installCall *packages.MockInstallCall
	for i := range calls {
		if calls[i].Method == "Install" {
			installCall = &calls[i]
			break
		}
	}
	if installCall == nil {
		t.Fatal("expected an Install call")
	}

	resp, ok := installCall.Args[3].(packages.Responses)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if resp["hostname"] == "" {
		t.Fatal("expected auto-generated hostname, got empty string")
	}
	if resp["port"] == "" {
		t.Fatal("expected auto-generated port, got empty string")
	}
}

func TestHTTPInstallValidationErrorsEmptyResponse(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Empty string responses for hostname and port types are now auto-generated,
	// so the install should succeed. Verify auto-generated values replaced the empties.
	err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{
		"hostname": "",
		"port":     "",
	}, false, "", false)
	if err != nil {
		t.Fatalf("expected install to succeed with auto-generated responses, got: %v", err)
	}

	calls := inst.GetCalls()
	var installCall *packages.MockInstallCall
	for i := range calls {
		if calls[i].Method == "Install" {
			installCall = &calls[i]
			break
		}
	}
	if installCall == nil {
		t.Fatal("expected an Install call")
	}

	resp, ok := installCall.Args[3].(packages.Responses)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if resp["hostname"] == "" {
		t.Fatal("expected auto-generated hostname, got empty string")
	}
	if resp["port"] == "" {
		t.Fatal("expected auto-generated port, got empty string")
	}
}

func TestHTTPInstallValidationErrorsUnknownQuestion(t *testing.T) {
	c, _ := initInstallTestClient(t)

	err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
		"bogus":    "value",
	}, false, "", false)
	if err == nil {
		t.Fatal("expected error from validation")
	}

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProblemError, got %T: %v", err, err)
	}

	if len(pe.ValidationErrors) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(pe.ValidationErrors), pe.ValidationErrors)
	}

	if pe.ValidationErrors[0].Name != "bogus" {
		t.Fatalf("expected error for 'bogus', got %q", pe.ValidationErrors[0].Name)
	}
	if pe.ValidationErrors[0].Error != packages.ErrInvalidResponse.Error() {
		t.Fatalf("expected %q, got %q", packages.ErrInvalidResponse.Error(), pe.ValidationErrors[0].Error)
	}
}

// --- Reinstall ---

func TestHTTPReinstallPackage(t *testing.T) {
	c, inst := initInstallTestClient(t)

	// Install first time.
	responses := packages.Responses{"hostname": "example", "port": "8080"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Reinstall with different responses.
	responses2 := packages.Responses{"hostname": "newhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", responses2, false, "", false); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	calls := inst.GetCalls()
	// First install: ListInstalled + ListInstalled + Install + ClearLastResponses = 4
	// Reinstall: ListInstalled + ListInstalled + GetResponses + Uninstall + Install + ClearLastResponses = 6
	// Total = 10
	if len(calls) != 10 {
		methods := make([]string, len(calls))
		for i, c := range calls {
			methods[i] = c.Method
		}
		t.Fatalf("expected 10 calls, got %d: %v", len(calls), methods)
	}

	// Reinstall phase starts at index 4.
	if calls[4].Method != "ListInstalled" {
		t.Fatalf("call 4: expected ListInstalled, got %q", calls[4].Method)
	}
	if calls[5].Method != "ListInstalled" {
		t.Fatalf("call 5: expected ListInstalled (port exclusion), got %q", calls[5].Method)
	}
	if calls[6].Method != "GetResponses" {
		t.Fatalf("call 6: expected GetResponses, got %q", calls[6].Method)
	}
	if calls[7].Method != "Uninstall" {
		t.Fatalf("call 7: expected Uninstall, got %q", calls[7].Method)
	}
	if calls[8].Method != "Install" {
		t.Fatalf("call 8: expected Install, got %q", calls[8].Method)
	}
	if calls[9].Method != "ClearLastResponses" {
		t.Fatalf("call 9: expected ClearLastResponses, got %q", calls[9].Method)
	}

	// Verify new responses were used.
	newResp, ok := calls[8].Args[3].(packages.Responses)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if newResp["hostname"] != "newhost" {
		t.Fatalf("expected hostname %q, got %q", "newhost", newResp["hostname"])
	}
}

func TestHTTPReinstallPackageWithSystemd(t *testing.T) {
	c, _, sd := initInstallWithSystemdTestClient(t)

	// Install first.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Reinstall.
	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "newhost"}, false, "", false); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	calls := sd.GetCalls()
	// First install: InstallUnit, Start = 2
	// Reinstall teardown: ListPackageUnitFiles, Stop, Disable, UninstallUnit = 4
	// Reinstall setup: InstallUnit, Start = 2
	// Total = 8
	if len(calls) != 8 {
		methods := make([]string, len(calls))
		for i, c := range calls {
			methods[i] = c.Method
		}
		t.Fatalf("expected 8 systemd calls, got %d: %v", len(calls), methods)
	}

	// Reinstall teardown: ListPackageUnitFiles, Stop, Disable, UninstallUnit
	if calls[2].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 2: expected ListPackageUnitFiles, got %q", calls[2].Method)
	}
	reinstallStop, ok := calls[3].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if reinstallStop != systemd.Stop {
		t.Fatalf("call 3: expected Stop, got %v", calls[3].Args[1])
	}
	reinstallDisable, ok := calls[4].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if reinstallDisable != systemd.Disable {
		t.Fatalf("call 4: expected Disable, got %v", calls[4].Args[1])
	}
	if calls[5].Method != "UninstallUnit" {
		t.Fatalf("call 5: expected UninstallUnit, got %q", calls[5].Method)
	}

	// Reinstall setup: InstallUnit, Start
	if calls[6].Method != "InstallUnit" {
		t.Fatalf("call 6: expected InstallUnit, got %q", calls[6].Method)
	}
	reinstallStart, ok := calls[7].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if reinstallStart != systemd.Start {
		t.Fatalf("call 7: expected Start, got %v", calls[7].Args[1])
	}
}

func TestHTTPInstallPackageNewVolumePaths(t *testing.T) {
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

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
  logs:
    mountpoint: /var/log/app
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	expectedData := "installed/repo-a/nginx/1.0/data"
	expectedLogs := "installed/repo-a/nginx/1.0/logs"
	if !found[expectedData] {
		t.Fatalf("expected filesystem %q, got: %v", expectedData, fs)
	}
	if !found[expectedLogs] {
		t.Fatalf("expected filesystem %q, got: %v", expectedLogs, fs)
	}
}

func TestHTTPUninstallPreservesVolumes(t *testing.T) {
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

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Uninstall WITHOUT purge.
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	// installed/repo-a/nginx should be gone (renamed to uninstalled/repo-a/nginx).
	for name := range found {
		if strings.HasPrefix(name, "installed/repo-a/nginx") {
			t.Fatalf("expected installed/repo-a/nginx volumes to be renamed away, found %q", name)
		}
	}

	// uninstalled/repo-a/nginx/... should exist.
	if !found["uninstalled/repo-a/nginx"] {
		t.Fatalf("expected uninstalled/repo-a/nginx to exist, got: %v", fs)
	}
	if !found["uninstalled/repo-a/nginx/1.0/data"] {
		t.Fatalf("expected uninstalled/repo-a/nginx/1.0/data to exist, got: %v", fs)
	}
}

func TestHTTPInstallWithVolumeReuse(t *testing.T) {
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

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install, then uninstall without purge (volumes move to uninstalled).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	// Verify volumes were moved to uninstalled.
	midFS := mockCtrl.GetFilesystems()
	midFound := map[string]bool{}
	for _, f := range midFS {
		midFound[f.Name] = true
	}
	if !midFound["uninstalled/repo-a/nginx"] {
		t.Fatalf("expected uninstalled/repo-a/nginx after uninstall, got: %v", midFS)
	}

	// Reinstall with reuseVolumes=true.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, true, "", false); err != nil {
		t.Fatalf("InstallPackage with reuse: %v", err)
	}

	// Verify volumes were renamed back from uninstalled to installed.
	afterFS := mockCtrl.GetFilesystems()
	afterFound := map[string]bool{}
	for _, f := range afterFS {
		afterFound[f.Name] = true
	}

	if !afterFound["installed/repo-a/nginx/1.0/data"] {
		t.Fatalf("expected installed/repo-a/nginx/1.0/data after reuse, got: %v", afterFS)
	}

	for name := range afterFound {
		if strings.HasPrefix(name, "uninstalled/repo-a/nginx") {
			t.Fatalf("expected no uninstalled/repo-a/nginx after reuse, found %q", name)
		}
	}
}

func TestHTTPInstallWithImport(t *testing.T) {
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

	// Install nginx 1.0 first.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Install nginx 2.0 with importFromVersion="1.0".
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{}, false, "1.0", false); err != nil {
		t.Fatalf("InstallPackage 2.0 with import: %v", err)
	}

	// Verify that the 2.0 volume exists (created via snapshot from 1.0).
	fs := mockCtrl.GetFilesystems()
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}

	expected20 := "installed/repo-a/nginx/2.0/data"
	if !found[expected20] {
		t.Fatalf("expected %q to exist after import, got: %v", expected20, fs)
	}

	// Verify snapshot was called by checking the mock controller log.
	callLog := mockCtrl.GetLog()
	snapshotFound := false
	for _, entry := range callLog {
		if entry.Operation == "SubvolSnapshot" {
			snapshotFound = true
			break
		}
	}
	if !snapshotFound {
		t.Fatal("expected SubvolSnapshot to be called for import")
	}
}

func TestHTTPPurgeUninstalledVolumes(t *testing.T) {
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

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Inject filesystems under uninstalled/repo-a/nginx/ to simulate preserved volumes.
	injectSubvol(t, mockCtrl, "uninstalled/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, mockCtrl, "uninstalled/repo-a/nginx/1.0/logs", 0)

	// Also inject a volume for a different package to ensure it is not affected.
	injectSubvol(t, mockCtrl, "uninstalled/repo-a/redis/7.0/data", 0)

	// Purge uninstalled volumes for nginx.
	if err := c.PurgeUninstalledVolumes(context.TODO(), "repo-a", "nginx"); err != nil {
		t.Fatalf("PurgeUninstalledVolumes: %v", err)
	}

	// Verify nginx uninstalled volumes are gone.
	fs := mockCtrl.GetFilesystems()
	for _, f := range fs {
		if strings.HasPrefix(f.Name, "uninstalled/repo-a/nginx") {
			t.Fatalf("expected uninstalled/repo-a/nginx volumes to be purged, found %q", f.Name)
		}
	}

	// Verify redis uninstalled volumes are intact.
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Name] = true
	}
	if !found["uninstalled/repo-a/redis/7.0/data"] {
		t.Fatalf("expected uninstalled/repo-a/redis/7.0/data to be preserved, got: %v", fs)
	}
}

// --- Older version selection tests ---

func TestHTTPInstallOlderVersion(t *testing.T) {
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

	// Explicitly install the older version 1.0 (not the latest 2.0).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	calls := inst.GetCalls()
	// Should have: ListInstalled, Install
	found := false
	for _, call := range calls {
		if call.Method == "Install" {
			installVer, ok := call.Args[2].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if installVer != "1.0" {
				t.Fatalf("expected install version %q, got %v", "1.0", call.Args[2])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected Install call, not found")
	}

	installed, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	foundInstalled := false
	for _, pkg := range installed.Entries {
		if pkg == "repo-a/nginx@1.0" {
			foundInstalled = true
		}
		if pkg == "repo-a/nginx@2.0" {
			t.Fatal("repo-a/nginx@2.0 should NOT be installed")
		}
	}
	if !foundInstalled {
		t.Fatalf("expected repo-a/nginx@1.0 in installed list, got: %v", installed.Entries)
	}
}

func TestHTTPInstallOlderVersionWithQuestions(t *testing.T) {
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
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
  port:
    query: "What external port?"
    type: port
`
	nginx20 := `image: nginx:2.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Fetch questions for the older version specifically.
	questions, err := c.GetPackageQuestionsByIdentity(context.TODO(), "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity 1.0: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions for nginx@1.0, got %d", len(questions))
	}
	if _, ok := questions["hostname"]; !ok {
		t.Fatal("expected 'hostname' question for nginx@1.0")
	}
	if _, ok := questions["port"]; !ok {
		t.Fatal("expected 'port' question for nginx@1.0")
	}

	// Fetch questions for the newer version — should have none.
	questions20, err := c.GetPackageQuestionsByIdentity(context.TODO(), "repo-a", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity 2.0: %v", err)
	}
	if len(questions20) != 0 {
		t.Fatalf("expected 0 questions for nginx@2.0, got %d", len(questions20))
	}

	// Install the older version with responses.
	responses := packages.Responses{"hostname": "myhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Verify the install recorded the correct version and responses.
	calls := inst.GetCalls()
	for _, call := range calls {
		if call.Method == "Install" {
			questionsVer, ok := call.Args[2].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if questionsVer != "1.0" {
				t.Fatalf("expected version 1.0, got %v", call.Args[2])
			}
			r, ok := call.Args[3].(packages.Responses)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if r["hostname"] != "myhost" {
				t.Fatalf("expected hostname=myhost, got %v", r["hostname"])
			}
			if r["port"] != "9090" {
				t.Fatalf("expected port=9090, got %v", r["port"])
			}
		}
	}
}

func TestHTTPInstallOlderVersionWithVolumes(t *testing.T) {
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
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
  logs:
    mountpoint: /var/log/nginx
questions: {}
`
	nginx20 := `image: nginx:2.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /var/lib/data
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx20)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install the older version 1.0 which has two volumes (data + logs).
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	fs := mockCtrl.GetFilesystems()
	volNames := map[string]bool{}
	for _, f := range fs {
		volNames[f.Name] = true
	}

	if !volNames["installed/repo-a/nginx/1.0/data"] {
		t.Fatal("expected volume installed/repo-a/nginx/1.0/data")
	}
	if !volNames["installed/repo-a/nginx/1.0/logs"] {
		t.Fatal("expected volume installed/repo-a/nginx/1.0/logs")
	}
	// Verify 2.0 volumes were NOT created.
	if volNames["installed/repo-a/nginx/2.0/data"] {
		t.Fatal("installed/repo-a/nginx/2.0/data should NOT exist")
	}
}

func TestHTTPInstallOlderVersionNotFound(t *testing.T) {
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

	// Try to install a version that doesn't exist.
	err = c.InstallPackage(context.TODO(), "nginx", "3.0", packages.Responses{}, false, "", false)
	if err == nil {
		t.Fatal("expected error installing nonexistent version 3.0")
	}
}

func TestHTTPListInstalledPagination(t *testing.T) {
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
	writeTestPackage(t, rr.BaseDir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Limit 1
	page, err := c.ListInstalled(context.TODO(), ListParams{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("ListInstalled page 0: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected 2 total pages, got %d", page.TotalPages)
	}

	// Second page
	page, err = c.ListInstalled(context.TODO(), ListParams{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListInstalled page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPDisablePackage(t *testing.T) {
	c, inst, sd := initInstallWithSystemdTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.DisablePackage(context.TODO(), "repo-a", "nginx"); err != nil {
		t.Fatalf("DisablePackage: %v", err)
	}

	instCalls := inst.GetCalls()
	found := false
	for _, call := range instCalls {
		if call.Method != "SetDisabled" {
			continue
		}
		disRepo, ok := call.Args[0].(string)
		if !ok {
			continue
		}
		disPkg, ok := call.Args[1].(string)
		if !ok {
			continue
		}
		disFlag, ok := call.Args[2].(bool)
		if !ok {
			continue
		}
		if disRepo == "repo-a" && disPkg == "nginx" && disFlag {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected SetDisabled(repo-a, nginx, true) call")
	}

	sdCalls := sd.GetCalls()
	lastCall := sdCalls[len(sdCalls)-1]
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastCall.Method != "SetStatus" || lastAction != systemd.Stop {
		t.Fatalf("expected last systemd call to be Stop, got %s %v", lastCall.Method, lastCall.Args)
	}
}

func TestHTTPEnablePackage(t *testing.T) {
	c, inst, sd := initInstallWithSystemdTestClient(t)

	if err := c.InstallPackage(context.TODO(),"nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.DisablePackage(context.TODO(), "repo-a", "nginx"); err != nil {
		t.Fatalf("DisablePackage: %v", err)
	}

	if err := c.EnablePackage(context.TODO(), "repo-a", "nginx"); err != nil {
		t.Fatalf("EnablePackage: %v", err)
	}

	instCalls := inst.GetCalls()
	found := false
	for _, call := range instCalls {
		if call.Method != "SetDisabled" {
			continue
		}
		enRepo, ok := call.Args[0].(string)
		if !ok {
			continue
		}
		enPkg, ok := call.Args[1].(string)
		if !ok {
			continue
		}
		enFlag, ok := call.Args[2].(bool)
		if !ok {
			continue
		}
		if enRepo == "repo-a" && enPkg == "nginx" && !enFlag {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected SetDisabled(repo-a, nginx, false) call")
	}

	sdCalls := sd.GetCalls()
	lastCall := sdCalls[len(sdCalls)-1]
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastCall.Method != "SetStatus" || lastAction != systemd.Start {
		t.Fatalf("expected last systemd call to be Start, got %s %v", lastCall.Method, lastCall.Args)
	}
}

func TestHTTPDowngradeFromNewerToOlderVersion(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	nginx := `image: nginx:latest
environment:
  NGINX_HOST: "@hostname@"
network:
  external: {}
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", nginx)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install the newer version first.
	if err := c.InstallPackage(context.TODO(), "nginx", "2.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0: %v", err)
	}

	// Now downgrade to the older version.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "testhost"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0 (downgrade): %v", err)
	}

	sdCalls := sd.GetCalls()
	// First install: InstallUnit + Start = 2
	// Downgrade teardown: ListPackageUnitFiles + Stop + Disable + UninstallUnit = 4
	// Downgrade setup: InstallUnit + Start = 2
	// Total = 8
	if len(sdCalls) != 8 {
		methods := make([]string, len(sdCalls))
		for i, cl := range sdCalls {
			methods[i] = cl.Method
		}
		t.Fatalf("expected 8 systemd calls, got %d: %v", len(sdCalls), methods)
	}

	// Downgrade teardown: ListPackageUnitFiles
	if sdCalls[2].Method != "ListPackageUnitFiles" {
		t.Fatalf("call 2: expected ListPackageUnitFiles, got %q", sdCalls[2].Method)
	}
	// Downgrade teardown: Stop old unit
	downgradeStop, ok := sdCalls[3].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if downgradeStop != systemd.Stop {
		t.Fatalf("call 3: expected Stop, got %v", sdCalls[3].Args[1])
	}
	// Downgrade teardown: Disable old unit
	downgradeDisable, ok := sdCalls[4].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if downgradeDisable != systemd.Disable {
		t.Fatalf("call 4: expected Disable, got %v", sdCalls[4].Args[1])
	}
	// Downgrade teardown: UninstallUnit
	if sdCalls[5].Method != "UninstallUnit" {
		t.Fatalf("call 5: expected UninstallUnit, got %q", sdCalls[5].Method)
	}
	// Downgrade setup: InstallUnit with 1.0 content
	if sdCalls[6].Method != "InstallUnit" {
		t.Fatalf("call 6: expected InstallUnit, got %q", sdCalls[6].Method)
	}
	unitContent, ok := sdCalls[6].Args[1].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if !strings.Contains(unitContent, "1.0") {
		t.Fatalf("expected unit content to reference version 1.0, got: %s", unitContent)
	}
	// Downgrade setup: Start new unit
	downgradeStart, ok := sdCalls[7].Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if downgradeStart != systemd.Start {
		t.Fatalf("call 7: expected Start, got %v", sdCalls[7].Args[1])
	}
}

func TestHTTPInstallPackageWithGitSeedVolume(t *testing.T) {
	// Installing a package with a git seed volume should succeed. The git
	// clone will fail (invalid URL) but is non-fatal and logged.
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

	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /etc/nginx/conf.d
    git: https://invalid.example.com/nonexistent/config.git
  html:
    mountpoint: /var/www/html
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "mysite", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install should succeed even though git clone will fail.
	if err := c.InstallPackage(context.TODO(), "mysite", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify volumes were created.
	fs := mockCtrl.GetFilesystems()
	foundConfig := false
	foundHTML := false
	for _, f := range fs {
		if f.Name == "installed/repo-a/mysite/1.0/config" {
			foundConfig = true
		}
		if f.Name == "installed/repo-a/mysite/1.0/html" {
			foundHTML = true
		}
	}
	if !foundConfig {
		t.Fatalf("expected config volume, got: %v", fs)
	}
	if !foundHTML {
		t.Fatalf("expected html volume, got: %v", fs)
	}

	// Verify Install was called.
	calls := inst.GetCalls()
	found := false
	for _, call := range calls {
		if call.Method == "Install" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected Install call in mock installer")
	}
}

func TestHTTPInstallPackageGitSeedOnlyVolume(t *testing.T) {
	// Package with only a git volume (no archive, no other volumes).
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	pkgYAML := `image: debian:latest
environment: {}
network:
  external: {}
  internal: {}
volumes:
  site:
    mountpoint: /var/www
    git: https://github.com/example/static-site.git
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "static", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "static", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Verify the install succeeded.
	calls := inst.GetCalls()
	installFound := false
	for _, call := range calls {
		if call.Method == "Install" {
			installFound = true
		}
	}
	if !installFound {
		t.Fatal("expected Install call")
	}
}

func TestHTTPInstallPackageRejectsInvalidGitURL(t *testing.T) {
	// Package with an invalid git URL should fail at compile time.
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// "not-a-url" has no scheme, so ValidateGitURL rejects it during Compile.
	pkgYAML := `image: nginx:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /config
    git: not-a-url
questions: {}
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "badgit", "1.0", pkgYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	err = c.InstallPackage(context.TODO(), "badgit", "1.0", packages.Responses{}, false, "", false)
	if err == nil {
		t.Fatal("expected error installing package with invalid git URL")
	}
}
