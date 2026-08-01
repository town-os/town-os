// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func gfehReconcileCtx(t *testing.T) context.Context {
	t.Helper()
	// Short: every partition's WaitForReady will time out against a mock
	// systemd (nothing actually binds a socket), and the reconcile treats that
	// as non-fatal. A long timeout here would just make the test slow.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// gfehTestRegistry builds a registry over mock managers and an in-memory
// network set.
func gfehTestRegistry(t *testing.T, networks ...string) (*gfehRegistry, *systemd.MockManager, *storage.BtrFS, string) {
	t.Helper()

	sd := systemd.InitMockManager()
	// The mock's BasePath is left empty on purpose. Its SubvolList echoes back
	// whatever SubvolCreate was handed, so a non-empty BasePath makes every
	// recorded name absolute -- and FilesystemNames filters on a relative
	// prefix, exactly as the real `btrfs subvolume list` output is relative to
	// the mount. Setting it here makes the mock disagree with production.
	//
	// The control directory is a separate temp dir, which is what
	// BtrfsBasePath below is for.
	st := storage.InitBtrFSMock()
	base := t.TempDir()

	nm := account.InitMockNetworkManager()
	for _, name := range networks {
		if _, err := nm.Create(&account.Network{Name: name, TLD: name, Enabled: true}); err != nil {
			t.Fatalf("create network %s: %v", name, err)
		}
	}

	reg := NewGfehRegistry(ReconcileGfehConfig{
		NetworkMgr:    nm,
		Storage:       st,
		Systemd:       sd,
		BtrfsBasePath: base,
		Image:         "localhost/town-os-gfeh:test",
		// A unique key prefix and podman network so a test's units can never
		// be mistaken for a production partition's — IRON RULE.
		KeyPrefix:   "test-",
		NetworkName: "town-os-ingress-test",
	})
	return reg, sd, st, base
}

// TestReconcileGfehCreatesOnePartitionPerNetwork is the shape of the whole
// feature: one partition per network, named for it.
func TestReconcileGfehCreatesOnePartitionPerNetwork(t *testing.T) {
	reg, _, _, base := gfehTestRegistry(t, "home", "office")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	managers := reg.Managers()
	if len(managers) != 2 {
		t.Fatalf("got %d partitions, want 2: %v", len(managers), managers)
	}
	for _, network := range []string{"home", "office"} {
		if _, ok := managers[network]; !ok {
			t.Errorf("no partition for network %q", network)
		}
		if _, err := os.Stat(gfeh.ConfigPath(base, network)); err != nil {
			t.Errorf("no rendered config for %q: %v", network, err)
		}
	}
}

// TestReconcileGfehCreatesTheSubvolumeWithGfehsOwnership. A bind mount passes
// host ownership straight through, so a partition owned by root is one the
// unprivileged daemon cannot write to.
func TestReconcileGfehCreatesTheSubvolumeWithGfehsOwnership(t *testing.T) {
	reg, _, st, _ := gfehTestRegistry(t, "home")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	controller, ok := st.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}
	owner, found := controller.GetOwners()[filepath.Join(st.BasePath, "gfeh/home")]
	if !found {
		t.Fatalf("the partition was not chowned; owners = %v", controller.GetOwners())
	}
	if owner.UID != gfeh.UID || owner.GID != gfeh.GID {
		t.Errorf("owner = %d:%d, want %d:%d", owner.UID, owner.GID, gfeh.UID, gfeh.GID)
	}
}

// TestReconcileGfehIsIdempotent is what stops the periodic reconcile bouncing
// every partition on the box: a second pass over an unchanged network set must
// not restart anything.
func TestReconcileGfehIsIdempotent(t *testing.T) {
	reg, sd, _, _ := gfehTestRegistry(t, "home")
	ctx := gfehReconcileCtx(t)

	ReconcileGfeh(ctx, reg)
	firstStops := countStops(sd)

	ReconcileGfeh(ctx, reg)
	secondStops := countStops(sd)

	if secondStops != firstStops {
		t.Errorf("a no-op reconcile restarted a partition: stops went %d -> %d", firstStops, secondStops)
	}
}

// TestReconcileGfehRestartsWhenTheConfigChanges is the other half: a real
// change has to take effect, or an operator enrolling an SMB credential would
// see nothing happen until the next reboot.
func TestReconcileGfehRestartsWhenTheConfigChanges(t *testing.T) {
	reg, sd, _, base := gfehTestRegistry(t, "home")
	ctx := gfehReconcileCtx(t)

	ReconcileGfeh(ctx, reg)
	before := countStops(sd)

	// Simulate a config that no longer matches what reconcile would render.
	if err := os.WriteFile(gfeh.ConfigPath(base, "home"), []byte("partition: stale\n"), 0o640); err != nil {
		t.Fatalf("write stale config: %v", err)
	}

	ReconcileGfeh(ctx, reg)
	if countStops(sd) == before {
		t.Error("a changed config did not restart the partition")
	}
}

// TestReconcileGfehPrunesAPartitionWhoseNetworkIsGone, and leaves its data
// alone: removing a network says nothing about the bytes stored under it, and
// deleting them would make a mistyped network name unrecoverable.
func TestReconcileGfehPrunesAPartitionWhoseNetworkIsGone(t *testing.T) {
	reg, sd, st, _ := gfehTestRegistry(t, "home", "office")
	ctx := gfehReconcileCtx(t)

	ReconcileGfeh(ctx, reg)
	if _, ok := reg.Managers()["office"]; !ok {
		t.Fatal("office was never provisioned")
	}

	nm, ok := reg.cfg.NetworkMgr.(*account.MockNetworkManager)
	if !ok {
		t.Fatal("expected *account.MockNetworkManager")
	}
	if err := nm.Remove("office"); err != nil {
		t.Fatalf("remove network: %v", err)
	}

	ReconcileGfeh(ctx, reg)

	if _, ok := reg.Managers()["office"]; ok {
		t.Error("the partition survived its network")
	}
	unit := systemd.SystemServiceUnitName("test-" + gfeh.ServiceKey("office"))
	if _, err := sd.ReadUnit(unit); err == nil {
		t.Error("the unit was not uninstalled")
	}

	// The subvolume stays.
	names, err := st.FilesystemNames(gfeh.VolumePrefix)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, n := range names {
		if n == "gfeh/office" {
			found = true
		}
	}
	if !found {
		t.Error("removing a network deleted its data; only /gfeh/partitions/remove may do that")
	}
}

// TestReconcileGfehSkipsSMBWithNoCredential. SMB with an empty credential table
// verifies nothing, so serving it would put an unauthenticated share on the LAN
// for every network on the box.
func TestReconcileGfehSkipsSMBWithNoCredential(t *testing.T) {
	reg, _, _, base := gfehTestRegistry(t, "home")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	rendered, err := os.ReadFile(gfeh.ConfigPath(base, "home"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(rendered), "smb:") {
		t.Errorf("SMB was served with nobody able to authenticate to it:\n%s", rendered)
	}
}

// TestCollectSMBUsersExpandsScope: a WireGuard account carries an explicit
// scope and an empty one is never "any"; any other account reaches every
// partition. Getting this backwards either locks everyone out or puts a scoped
// account on every network's share.
func TestCollectSMBUsersExpandsScope(t *testing.T) {
	mgr := account.InitMockManager()
	all := []string{"home", "office"}

	mustAccount(t, mgr, "alice", false, nil)
	mustAccount(t, mgr, "scoped", true, []string{"office"})
	mustAccount(t, mgr, "nocred", false, nil)

	setSMBCredential(t, mgr, "alice")
	setSMBCredential(t, mgr, "scoped")
	// nocred deliberately has none.

	users := collectSMBUsers(mgr, all)

	if got := usernames(users["home"]); len(got) != 1 || got[0] != "alice" {
		t.Errorf("home users = %v, want [alice]", got)
	}
	got := usernames(users["office"])
	if len(got) != 2 || got[0] != "alice" || got[1] != "scoped" {
		t.Errorf("office users = %v, want [alice scoped] sorted", got)
	}
}

// TestCollectSMBUsersExcludesDisabledAccounts: a disabled account keeps its
// credential row but must not be able to authenticate, matching the rule
// requireAuth applies to a live session.
func TestCollectSMBUsersExcludesDisabledAccounts(t *testing.T) {
	mgr := account.InitMockManager()
	mustAccount(t, mgr, "alice", false, nil)
	setSMBCredential(t, mgr, "alice")

	if got := usernames(collectSMBUsers(mgr, []string{"home"})["home"]); len(got) != 1 {
		t.Fatalf("precondition: alice should be enrolled, got %v", got)
	}
	if err := mgr.Disable("alice"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := usernames(collectSMBUsers(mgr, []string{"home"})["home"]); len(got) != 0 {
		t.Errorf("a disabled account can still mount the share: %v", got)
	}
}

// TestGfehNetworkFromUnit is what the teardown pass has to work from.
func TestGfehNetworkFromUnit(t *testing.T) {
	for _, tc := range []struct {
		unit, prefix, want string
		ok                 bool
	}{
		{"town-os-system--gfeh-office.service", "", "office", true},
		{"town-os-system--test-gfeh-office.service", "test-", "office", true},
		{"town-os-system--ingress.service", "", "", false},
		{"town-os-package--core-nginx.service", "", "", false},
		{"town-os-system--gfeh-office.service", "test-", "", false},
	} {
		got, ok := gfehNetworkFromUnit(tc.unit, tc.prefix)
		if ok != tc.ok || got != tc.want {
			t.Errorf("gfehNetworkFromUnit(%q, %q) = (%q, %v), want (%q, %v)", tc.unit, tc.prefix, got, ok, tc.want, tc.ok)
		}
	}
}

// TestWriteGfehConfigOnlyReportsRealChanges. The return value decides whether
// the daemon restarts, so a byte-identical rewrite must report false.
func TestWriteGfehConfigOnlyReportsRealChanges(t *testing.T) {
	path := t.TempDir() + "/gfehd.yaml"

	changed, err := writeGfehConfig(path, []byte("a: 1\n"))
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	changed, err = writeGfehConfig(path, []byte("a: 1\n"))
	if err != nil || changed {
		t.Errorf("identical rewrite reported a change: changed=%v err=%v", changed, err)
	}
	changed, err = writeGfehConfig(path, []byte("a: 2\n"))
	if err != nil || !changed {
		t.Errorf("a real change was not reported: changed=%v err=%v", changed, err)
	}
}

// TestWriteGfehConfigIsNotWorldReadable: the file carries NT hashes, which are
// password-equivalent for SMB.
func TestWriteGfehConfigIsNotWorldReadable(t *testing.T) {
	path := t.TempDir() + "/gfehd.yaml"
	if _, err := writeGfehConfig(path, []byte("a: 1\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o007 != 0 {
		t.Errorf("mode %v is world-accessible; the file holds SMB credentials", info.Mode().Perm())
	}
}

// --- helpers ---

// countStops counts the Stop calls a reconcile made.
//
// Stop is the signal that a partition was restarted rather than left alone:
// Manager.Start only stops first when the caller asked for a restart.
func countStops(sd *systemd.MockManager) int {
	n := 0
	for _, call := range sd.GetCalls() {
		if call.Method != "SetStatus" || len(call.Args) != 2 {
			continue
		}
		if got, ok := call.Args[1].(systemd.StatusAction); ok && got == systemd.Stop {
			n++
		}
	}
	return n
}

func usernames(users []gfeh.SmbUserConfig) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.Username)
	}
	return out
}

func mustAccount(t *testing.T, mgr account.Manager, username string, wireguard bool, networks []string) {
	t.Helper()
	var err error
	if wireguard {
		_, err = mgr.CreateWireGuard(username, "hunter2hunter2", username+"@example.com", "5551234", username, networks)
	} else {
		_, err = mgr.Create(username, "hunter2hunter2", username+"@example.com", "5551234", username, false)
	}
	if err != nil {
		t.Fatalf("create account %s: %v", username, err)
	}
}

func setSMBCredential(t *testing.T, mgr account.Manager, username string) {
	t.Helper()
	pw := "smbpassword"
	if _, err := mgr.Update(username, account.UpdateFields{SMBPassword: &pw}); err != nil {
		t.Fatalf("set smb credential for %s: %v", username, err)
	}
}


