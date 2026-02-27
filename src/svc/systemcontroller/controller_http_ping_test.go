package systemcontroller

import (
	"context"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPPingIncludesAccountCount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// 2 = testadmin (bootstrap) + alice
	if ping.Accounts != 2 {
		t.Fatalf("expected 2 accounts in ping, got %d", ping.Accounts)
	}

	// only testadmin is an enabled admin
	if ping.Admins != 1 {
		t.Fatalf("expected 1 admin in ping, got %d", ping.Admins)
	}
}

func TestHTTPPingUnitCountsFiltersTownOS(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Register installed packages that match the systemd units.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
		{Repo: "repo", Name: "redis", Version: "1.0"},
		{Repo: "repo", Name: "postgres", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-redis-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-postgres-1.0.service", ActiveState: "failed"},
		{Name: "town-os-systemcontroller.service", ActiveState: "active"},
		{Name: "sshd.service", ActiveState: "active"},
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 3 {
		t.Fatalf("expected 3 total town-os units, got %d", ping.Units.Total)
	}

	if ping.Units.Active != 2 {
		t.Fatalf("expected 2 active town-os units, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 1 {
		t.Fatalf("expected 1 failed town-os unit, got %d", ping.Units.Failed)
	}
}

func TestHTTPPingUnitCountsExcludesUninstalledPackages(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Only nginx is installed; redis unit exists but has no install record.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}

	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-redis-1.0.service", ActiveState: "active"},
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 1 {
		t.Fatalf("expected 1 total unit (only installed), got %d", ping.Units.Total)
	}

	if ping.Units.Active != 1 {
		t.Fatalf("expected 1 active unit, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 0 {
		t.Fatalf("expected 0 failed units, got %d", ping.Units.Failed)
	}
}

func TestHTTPPingUnitCountsZeroWithNoInstalledPackages(t *testing.T) {
	c, sd, _ := initSystemdTestClient(t)

	// No packages installed but units exist (e.g. leftover from uninstall).
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--repo-redis-1.0.service", ActiveState: "failed"},
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.Units == nil {
		t.Fatal("expected units in ping response")
	}

	if ping.Units.Total != 0 {
		t.Fatalf("expected 0 total units with no installed packages, got %d", ping.Units.Total)
	}

	if ping.Units.Active != 0 {
		t.Fatalf("expected 0 active units, got %d", ping.Units.Active)
	}

	if ping.Units.Failed != 0 {
		t.Fatalf("expected 0 failed units, got %d", ping.Units.Failed)
	}
}

func TestHTTPPingNeedsSetup(t *testing.T) {
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

	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, AccountMgr: mgr, SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// No accounts — needs_setup should be true.
	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Fatal("expected needs_setup=true with no accounts")
	}

	// Create admin (no auth needed on empty DB).
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Admin exists but no session — needs_setup should still be true.
	ping, err = c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Fatal("expected needs_setup=true with admin but no active sessions")
	}

	// Authenticate to create a session.
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Now needs_setup should be false.
	ping, err = c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.NeedsSetup {
		t.Fatal("expected needs_setup=false with active admin session")
	}
}

func TestHTTPPingIncludesUpgradesAvailable(t *testing.T) {
	c, _ := initUpgradesTestServer(t)

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.UpgradesAvailable != 1 {
		t.Fatalf("expected 1 upgrade available, got %d", ping.UpgradesAvailable)
	}
	if ping.UpgradesDismissed {
		t.Fatal("expected upgrades not dismissed initially")
	}
}
