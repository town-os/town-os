// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func scRepoCredentials() (string, string) {
	return os.Getenv(packages.EnvRepoUsername), os.Getenv(packages.EnvRepoPassword)
}

// ncTestImage returns the network controller image injected by the test
// harness (built on the host by the nc-image make target and loaded into the
// test container). The tag is per-instance, so it must never be hardcoded.
// The fallback only applies outside the harness, where no NC container can
// actually start anyway (mock-systemd tests just need a stable string).
func ncTestImage() string {
	if img := os.Getenv("NC_IMAGE"); img != "" {
		return img
	}
	return "localhost/town-os-networkcontroller:local"
}

func addRepoWithCreds(c *systemcontroller.SystemdClient, name, rawURL string) error {
	user, pass := scRepoCredentials()
	return c.AddRepository(context.TODO(), name, rawURL, user, pass)
}

func initSystemControllerTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()
	c, _ := initSystemControllerTestWithStorage(t)
	return c
}

func initSystemControllerTestWithStorage(t *testing.T) (*systemcontroller.SystemdClient, *storage.BtrFS) {
	t.Helper()

	btr := storage.InitBtrFS("/town-os")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: btr})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, btr
}

func initSystemControllerRepoTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func initSystemControllerInstallTest(t *testing.T) (*systemcontroller.SystemdClient, *packages.RepositoryRoot) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	inst := packages.NewInstallManager(dir)

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, rr
}

func initSystemControllerInstallSystemdTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd
}

func initSystemControllerSystemdTest(t *testing.T, sd *systemd.MockManager, installed ...packages.PackageIdentity) *systemcontroller.SystemdClient {
	t.Helper()

	btr := storage.InitBtrFS("/town-os")
	inst := packages.InitMockInstallManager()
	inst.Installed = installed
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: btr, Installer: inst, Systemd: sd})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

// realSystemdFixture bundles the per-test artifacts returned by
// initRealSystemdTest: the HTTP client, the installed unit name, and the
// distinctive log message the unit echoes when started. Each field is
// unique per invocation so multiple tests can share the real systemd
// bus in parallel without clobbering each other's state.
type realSystemdFixture struct {
	Client   *systemcontroller.SystemdClient
	UnitName string
	Message  string
}

// initRealSystemdTest installs a unique oneshot systemd unit via the real
// systemd manager and returns a client + identifiers for the unit. Each
// test gets its own randomized PackageIdentity so tests can run in
// parallel against the shared system bus. The unit logs a per-test
// unique message via /bin/echo so log-replay tests can match it
// unambiguously.
func initRealSystemdTest(t *testing.T) realSystemdFixture {
	t.Helper()

	suffix := strconv.FormatUint(rand.Uint64(), 36)
	identity := packages.PackageIdentity{
		Repo:    "realtest",
		Name:    "unit-" + suffix,
		Version: "1.0",
	}
	unitName := systemd.UnitName(identity.Repo, identity.Name, identity.Version)
	message := "town-os-realtest-msg-" + suffix

	sd := systemd.NewManager()
	ctx := context.Background()

	// Minimal oneshot unit that echoes a unique marker so log-replay
	// tests can find it in the journal.
	unitContent := fmt.Sprintf(`[Unit]
Description=Town OS Real Systemd Test: %s

[Service]
Type=oneshot
ExecStart=/bin/echo %s
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`, identity.Name, message)

	if err := sd.InstallUnit(ctx, unitName, unitContent); err != nil {
		t.Fatalf("InstallUnit %s: %v", unitName, err)
	}
	// Enable so multi-user.target's Wants= keeps the unit loaded in
	// systemd's memory even when it's inactive. Without this,
	// unreferenced inactive units are GC'd and vanish from ListUnits,
	// breaking tests that Stop and then re-list the unit. Enable does
	// not start the unit — it just creates the symlink + daemon-reload.
	if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
		t.Fatalf("Enable %s: %v", unitName, err)
	}
	// Registered before ts.Close below so (LIFO) ts.Close runs first,
	// then the unit is stopped and removed.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if err := sd.SetStatus(cleanupCtx, unitName, systemd.Stop); err != nil {
			t.Logf("cleanup SetStatus(%s, stop): %v", unitName, err)
		}
		if err := sd.SetStatus(cleanupCtx, unitName, systemd.Disable); err != nil {
			t.Logf("cleanup SetStatus(%s, disable): %v", unitName, err)
		}
		if err := sd.UninstallUnit(cleanupCtx, unitName); err != nil {
			t.Logf("cleanup UninstallUnit(%s): %v", unitName, err)
		}
	})

	mock := storage.InitBtrFSMock()
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{identity}
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:   mock,
		Systemd:   sd,
		Installer: inst,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return realSystemdFixture{
		Client:   c,
		UnitName: unitName,
		Message:  message,
	}
}

func initSystemControllerSettingsTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	db, err := account.OpenDB(t.Context(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	auditMgr, err := account.InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	settingsMgr, err := account.InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	btr := storage.InitBtrFS("/town-os")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:     btr,
		AccountMgr:  mgr,
		SessionMgr:  sessMgr,
		AuditMgr:    auditMgr,
		SettingsMgr: settingsMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap: create admin account and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c
}

func initBootstrapTest(t *testing.T) (*systemcontroller.SystemdClient, *account.SQLiteManager) {
	t.Helper()

	dir := t.TempDir()
	db, err := account.OpenDB(t.Context(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    mock,
		AccountMgr: mgr,
		SessionMgr: sessMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, mgr
}

func initReconcileTest(t *testing.T) (
	*systemcontroller.SystemdClient,
	*packages.RepositoryRoot,
	packages.Installer,
	*systemd.MockManager,
	storage.Storage,
) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, rr, inst, sd, mock
}

func initSystemControllerInstallSystemdTestWithNetworkState(t *testing.T) (*systemcontroller.SystemdClient, string) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	netStateDir := filepath.Join(dir, "network-state")
	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:                mock,
		RepositoryRoot:         rr,
		Installer:              inst,
		Systemd:                sd,
		NetworkControllerImage: ncTestImage(),
		NetworkStatePath:       netStateDir,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, netStateDir
}



func initSystemControllerTestWithBtrfsBase(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()
	btr := storage.InitBtrFS("/town-os")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       btr,
		BtrfsBasePath: "/town-os",
	})
	t.Cleanup(func() { ts.Server.Close() })
	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}
	return c
}

func initSystemControllerTestWithStorageAndBtrfsBase(t *testing.T) (*systemcontroller.SystemdClient, *storage.BtrFS) {
	t.Helper()

	btr := storage.InitBtrFS("/town-os")
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       btr,
		BtrfsBasePath: "/town-os",
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, btr
}

func findFreePort(t *testing.T) string {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", ":0")
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("findFreePort: unexpected address type")
	}
	port := addr.Port
	if err := ln.Close(); err != nil {
		t.Fatalf("findFreePort close: %v", err)
	}

	return strconv.Itoa(port)
}

// mockSettingsManager is a minimal in-memory settings manager for integration
// tests that need to configure system settings like proton_image.
type mockSettingsManager struct {
	values map[string]string
}

func (m *mockSettingsManager) Get(_ context.Context, key string) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", fmt.Errorf("setting %q not found", key)
	}
	return v, nil
}

func (m *mockSettingsManager) Set(_ context.Context, key, value string) error {
	m.values[key] = value
	return nil
}

func (m *mockSettingsManager) List(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(m.values))
	maps.Copy(out, m.values)
	return out, nil
}
