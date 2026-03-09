// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func testRoute(t *testing.T, base, path string) string {
	t.Helper()
	u, err := url.JoinPath(base, path)
	if err != nil {
		t.Fatalf("url.JoinPath(%q, %q): %v", base, path, err)
	}
	return u
}

func initTestClient(t *testing.T) (*SystemdClient, *storage.MockBtrFSController) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("type assertion failed")
	}
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, controller
}

// injectSubvol creates a subvolume and all intermediate parents in the mock
// controller, mimicking what storage.CreateFilesystem does automatically.
// Existing subvolumes are not duplicated.
func injectSubvol(t *testing.T, ctrl *storage.MockBtrFSController, name string, quota uint64) {
	t.Helper()
	parts := strings.Split(name, "/")
	for i := 1; i <= len(parts); i++ {
		intermediate := strings.Join(parts[:i], "/")
		exists := false
		for _, fs := range ctrl.GetFilesystems() {
			if fs.Name == intermediate {
				exists = true
				break
			}
		}
		if !exists {
			if err := ctrl.SubvolCreate(intermediate); err != nil {
				t.Fatalf("SubvolCreate %q: %v", intermediate, err)
			}
		}
	}
	if quota > 0 {
		if err := ctrl.QGroupLimit(name, quota); err != nil {
			t.Fatalf("QGroupLimit %q: %v", name, err)
		}
	}
}

func emptyRepoRoot(t *testing.T) *packages.RepositoryRoot {
	t.Helper()
	dir := t.TempDir()
	return &packages.RepositoryRoot{BaseDir: dir, Git: &git.GoGitClient{Home: dir}}
}

func writeTestPackage(t *testing.T, baseDir, repoName, pkgName, version, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, repoName, packages.PackagesDir, pkgName)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("os.MkdirAll %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("os.WriteFile %s/%s.yaml: %v", dir, version, err)
	}
}

func initInstallTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager) {
	t.Helper()
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
    query: "What external port should nginx listen on?"
    type: port
notes:
  URL:
    value: "http://@hostname@:@port@"
    type: url
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst
}

func initInstallWithVolumesTestClient(t *testing.T) (*SystemdClient, *storage.MockBtrFSController) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("type assertion failed")
	}
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
volumes:
  html:
    mountpoint: /var/www/html
  logs:
    mountpoint: /var/log/nginx
    quota: 2048
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, controller
}

func initInstallWithSystemdTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, *systemd.MockManager) {
	t.Helper()
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
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, sd
}

func initAccountTestClient(t *testing.T) (*SystemdClient, account.AuditManager) {
	t.Helper()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr, AuditMgr: auditMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account (no auth required on empty DB) and authenticate
	if _, err := c.CreateAccount(context.TODO(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Test Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, auditMgr
}

func initSystemdTestClient(t *testing.T) (*SystemdClient, *systemd.MockManager, *packages.MockInstallManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, sd, inst
}

func initSystemdTestClientWithRepoRoot(t *testing.T) (*SystemdClient, *systemd.MockManager, *packages.MockInstallManager, *packages.RepositoryRoot) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	inst := packages.InitMockInstallManager()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd, Installer: inst, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, sd, inst, rr
}

func initSettingsTestClient(t *testing.T) (*SystemdClient, string) {
	t.Helper()
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr, AuditMgr: auditMgr, SettingsMgr: settingsMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account and authenticate
	if _, err := c.CreateAccount(context.TODO(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Test Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c, resp.Token
}

func initInstallTestClientWithAuth(t *testing.T) (*SystemdClient, *packages.MockInstallManager) { //nolint:unparam // callers may use either return value
	t.Helper()
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
    query: "What external port should nginx listen on?"
    type: port
notes:
  URL:
    value: "http://@hostname@:@port@"
    type: url
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account first (required on empty DB).
	if _, err := c.CreateAccount(context.TODO(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Test Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	// Authenticate admin to create a session (needed for needs_setup).
	if _, err := c.Authenticate(context.TODO(), "testadmin", "adminpass"); err != nil {
		t.Fatalf("bootstrap admin Authenticate: %v", err)
	}

	// Create non-admin user and authenticate as that user.
	resp, err := c.Authenticate(context.TODO(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = resp.Token

	if _, err := c.CreateAccount(context.TODO(), "regularuser", "password1", "user@test.com", "555-1111", "Regular User", false); err != nil {
		t.Fatalf("CreateAccount regularuser: %v", err)
	}
	userResp, err := c.Authenticate(context.TODO(), "regularuser", "password1")
	if err != nil {
		t.Fatalf("Authenticate regularuser: %v", err)
	}
	c.Token = userResp.Token

	return c, inst
}

func initMultiRepoTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
		{Name: "repo-b", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-b", "nginx", "1.0", "image: nginx:1.0-alt\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst
}

func initInstallWithGitTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, *packages.MockGitCloner) { //nolint:unparam // callers may use any return value
	t.Helper()
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
environment: {}
network:
  external: {}
  internal: {}
volumes:
  site:
    mountpoint: /var/www/html
questions: {}
git_sources:
  - url: https://example.com/repo.git
    branch: main
    volume: site
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

	inst := packages.InitMockInstallManager()
	gitCloner := &packages.MockGitCloner{}
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, GitCloner: gitCloner})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, gitCloner
}

func initProtonInstallTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}
	protonPkg := `image:
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
	writeTestPackage(t, rr.BaseDir, "repo-a", "winapp", "1.0", protonPkg)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()

	settingsMgr := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	ts := InitTestServer(ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, sd
}

func initInstallWithGitTemplateTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, *packages.MockGitCloner) {
	t.Helper()
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
environment: {}
network:
  external: {}
  internal: {}
volumes:
  site:
    mountpoint: /var/www/html
questions:
  repourl:
    query: "Git repository URL?"
git_sources:
  - url: "@repourl@"
    branch: main
    volume: site
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

	inst := packages.InitMockInstallManager()
	gitCloner := &packages.MockGitCloner{}
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, GitCloner: gitCloner})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, gitCloner
}

// initInstallWithTemplatesTestClient creates a test server with a package
// containing templates and volumes. Returns the client, install manager, and
// the btrfs base path (a temp dir) where template files are written.
func initInstallWithTemplatesTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, string) { //nolint:unparam // consistent with other init helpers
	t.Helper()
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
volumes:
  config:
    mountpoint: /etc/nginx
  data:
    mountpoint: /var/data
questions:
  hostname:
    query: "What hostname should nginx serve?"
    type: hostname
templates:
  nginx_conf:
    volume: config
    path: nginx.conf
    content: "server_name {{.Responses.hostname}};"
  readme:
    volume: data
    path: info.txt
    content: "{{.Package.Name}} v{{.Package.Version}}"
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", nginx10)

	btrfsBase := t.TempDir()
	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, BtrfsBasePath: btrfsBase})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, btrfsBase
}

func initUpgradesTestServer(t *testing.T) (*SystemdClient, *packages.InstallManager) {
	t.Helper()
	dir := t.TempDir()

	// Set up repo with two versions of a package.
	repoName := "test-repo"
	pkgName := "nginx"
	pkgDir := filepath.Join(dir, repoName, packages.PackagesDir, pkgName)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile 1.0: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "2.0.yaml"), []byte("image: nginx:2.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile 2.0: %v", err)
	}

	// Write repositories file.
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

	// Install version 1.0.
	if err := inst.Install(repoName, pkgName, "1.0", packages.Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := account.OpenDB(dbFile)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { if err := db.Close(); err != nil { t.Errorf("db.Close: %v", err) } })

	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	ts := InitTestServer(ServerConfig{
		RepositoryRoot: rr,
		Installer:      inst,
		SettingsMgr:    settingsMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	return c, inst
}
