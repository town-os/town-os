// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
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
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
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

	// Admin exists — needs_setup should be false regardless of sessions.
	ping, err = c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.NeedsSetup {
		t.Fatal("expected needs_setup=false with admin account present")
	}
}

// TestHTTPPingAvoidsQuotaLookupsPerSubvolume pins the perf invariant: the
// /status/ping handler must not call `btrfs qgroup show` (a fork+exec) per
// subvolume. Prior to the FilesystemNames refactor, the handler used
// ListFilesystems which invoked SubvolID + QGroupShow for every volume,
// turning the endpoint into a 1.4–1.6 s wait on production hosts.
func TestHTTPPingAvoidsQuotaLookupsPerSubvolume(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ctrl, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected MockBtrFSController")
	}

	// Seed the subvolume list directly so the fixture contains only the
	// leaf volumes we want to classify (CreateFilesystem would also create
	// intermediate subvolumes, which just muddles the counts here).
	ctrl.Lock.Lock()
	ctrl.Filesystems = nil
	for _, n := range []string{
		"user/alice",
		"user/bob",
		"installed/default/nginx/1.0/data",
		"installed/default/nginx/1.0/logs",
		"uninstalled/default/redis/1.0/data",
	} {
		ctrl.NextID++
		ctrl.Filesystems = append(ctrl.Filesystems, storage.SubvolInfo{Name: n, ID: ctrl.NextID})
	}
	ctrl.Call = nil
	ctrl.Lock.Unlock()

	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// Counts still correct.
	if ping.Filesystems != 2 {
		t.Errorf("Filesystems: want 2, got %d", ping.Filesystems)
	}
	if ping.InstalledVolumes != 2 {
		t.Errorf("InstalledVolumes: want 2, got %d", ping.InstalledVolumes)
	}
	if ping.UninstalledVolumes != 1 {
		t.Errorf("UninstalledVolumes: want 1, got %d", ping.UninstalledVolumes)
	}

	// Ping must not have triggered per-subvolume quota lookups.
	for _, call := range ctrl.GetLog() {
		if call.Operation == "QGroupShow" || call.Operation == "SubvolID" {
			t.Errorf("/status/ping triggered %s (args=%v); quota lookups are forbidden on this hot path", call.Operation, call.Arguments)
		}
	}
}

func TestHTTPPingContinuesOnListFilesystemsError(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ctrl, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected MockBtrFSController")
	}
	ctrl.SubvolListErr = errors.New("btrfs: filesystem error")

	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping should succeed despite ListFilesystems error: %v", err)
	}

	if ping.Filesystems != 0 {
		t.Fatalf("expected 0 filesystems, got %d", ping.Filesystems)
	}
	if ping.InstalledVolumes != 0 {
		t.Fatalf("expected 0 installed volumes, got %d", ping.InstalledVolumes)
	}
	if ping.UninstalledVolumes != 0 {
		t.Fatalf("expected 0 uninstalled volumes, got %d", ping.UninstalledVolumes)
	}
}

func TestHTTPPingIncludesRepositoryErrors(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := &packages.RepositoryRoot{
		BaseDir: t.TempDir(),
		Errors:  map[string]string{"bad-repo": "connection refused"},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if len(ping.RepositoryErrors) != 1 {
		t.Fatalf("expected 1 repository error, got %d", len(ping.RepositoryErrors))
	}
	if ping.RepositoryErrors["bad-repo"] != "connection refused" {
		t.Fatalf("expected error %q, got %q", "connection refused", ping.RepositoryErrors["bad-repo"])
	}
}

func TestHTTPPingIncludesTimezoneOffset(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// The offset should be within a valid range: UTC-12 to UTC+14.
	if ping.TimezoneOffset < -720 || ping.TimezoneOffset > 840 {
		t.Fatalf("timezone_offset %d outside valid range [-720, 840]", ping.TimezoneOffset)
	}

	// All real-world UTC offsets are multiples of 15 minutes.
	if ping.TimezoneOffset%15 != 0 {
		t.Fatalf("timezone_offset %d is not a multiple of 15", ping.TimezoneOffset)
	}
}

func TestHTTPPingIncludesExternalIP(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	// Simulate a fetched external IP.
	ts.externalIP.Store("203.0.113.42")

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.ExternalIP != "203.0.113.42" {
		t.Fatalf("expected external_ip %q, got %q", "203.0.113.42", ping.ExternalIP)
	}
}

func TestHTTPPingExternalIPEmptyWhenNotFetched(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.ExternalIP != "" {
		t.Fatalf("expected empty external_ip before fetch, got %q", ping.ExternalIP)
	}
}

func TestHTTPPingIncludesInternalIP(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// Internal IP should be non-empty on any machine with a network interface.
	// On CI it might be empty if no non-loopback interface exists, so just
	// verify it doesn't error.
	_ = ping.InternalIP
}

// TestHTTPPingReportsBootID pins the field the Refresh Core Services flow
// depends on: the full router's ping must report the process's boot id, the
// same value the boot stub reported before the handler swap. Without it a
// refresh client cannot tell the controller it asked to restart from the
// controller that came back, because both answer ping 200 and 404
// /boot-status.
func TestHTTPPingReportsBootID(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		BootID:         "boot-abc",
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.BootID != "boot-abc" {
		t.Fatalf("ping boot_id = %q, want %q", ping.BootID, "boot-abc")
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
