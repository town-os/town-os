// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
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

	// Seed rather than Create: the home network is already there (every network
	// manager has one), and a fixture naming it means "this is the row", not
	// "add a row".
	nm := account.InitMockNetworkManager()
	for _, name := range networks {
		nm.Seed(&account.Network{Name: name, TLD: name, Enabled: true})
	}

	reg := NewGfehRegistry(ReconcileGfehConfig{
		NetworkMgr: nm,
		Storage:    st,
		Systemd:    sd,
		// Object storage has no on/off setting, so the manager starts empty.
		// The always-on tests below add keys to prove none of them matter.
		SettingsMgr: &mockSettingsManager{values: map[string]string{}},
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
// change has to take effect, or an operator changing a partition setting would
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
	if err := nm.Remove(t.Context(), "office"); err != nil {
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
// the daemon's whole configuration.
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
		t.Errorf("mode %v is world-accessible; the file holds the partition's credentials", info.Mode().Perm())
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

// mustAccount creates a Town OS account for a test to project into a partition.
//
// admin is the one axis that matters here: it decides the ceiling
// gfeh.CeilingForAccount projects, and therefore what a grant can be clamped to
// on the other side.
func mustAccount(t *testing.T, mgr account.Manager, username string, admin bool) {
	t.Helper()
	if _, err := mgr.Create(username, "hunter2hunter2", username+"@example.com", "5551234", username, admin); err != nil {
		t.Fatalf("create account %s: %v", username, err)
	}
}

// --- Always on ---

// Object storage has no switch. It runs the way DNS and the ingress run: as
// part of what Town OS is. A settings manager holding nothing at all must
// therefore still bring the partitions up -- the old behaviour, where an absent
// or unreadable decision meant "off", would leave a box with no object storage
// and nothing on screen explaining why.
func TestReconcileGfehRunsWithNoSettingsAtAll(t *testing.T) {
	reg, _, _, base := gfehTestRegistry(t, "home", "office")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	if managers := reg.Managers(); len(managers) != 2 {
		t.Fatalf("expected both partitions, got %v", managers)
	}
	for _, network := range []string{"home", "office"} {
		if _, err := os.Stat(gfeh.ConfigPath(base, network)); err != nil {
			t.Errorf("no config rendered for %q: %v", network, err)
		}
	}
}

// Several controller tests build a registry with no settings manager at all.
// That used to read as "off"; it now reads as "on, with no quota", which is the
// only answer that keeps object storage from silently depending on a manager
// that was never the thing deciding whether it should exist.
func TestReconcileGfehRunsWithNoSettingsManager(t *testing.T) {
	reg, _, _, base := gfehTestRegistry(t, "home")
	reg.cfg.SettingsMgr = nil

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	if managers := reg.Managers(); len(managers) != 1 {
		t.Fatalf("expected one partition, got %v", managers)
	}
	if _, err := os.Stat(gfeh.ConfigPath(base, "home")); err != nil {
		t.Errorf("no config rendered: %v", err)
	}
}

// A box upgraded from the release that had the switch still has the row in its
// settings table -- seeding never deletes keys -- and it may well say "false".
// Nothing reads it, and a partition must not stay down because of a decision
// about a control that no longer exists.
func TestReconcileGfehIgnoresAStaleOffSetting(t *testing.T) {
	reg, _, _, base := gfehTestRegistry(t, "home")
	if err := reg.cfg.SettingsMgr.Set(t.Context(), "object_storage_enabled", "false"); err != nil {
		t.Fatalf("seed stale setting: %v", err)
	}

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	if managers := reg.Managers(); len(managers) != 1 {
		t.Fatalf("a stale setting kept object storage down: %v", managers)
	}
	if _, err := os.Stat(gfeh.ConfigPath(base, "home")); err != nil {
		t.Errorf("no config rendered: %v", err)
	}
}

// --- SMB ---

// Town OS accounts carry no SMB password, so there is nobody gfehd could
// authenticate to a share and the view is not served at all. The alternative --
// an unauthenticated share on the LAN -- is not a fallback worth having.
func TestReconcileGfehNeverServesSMB(t *testing.T) {
	reg, _, _, base := gfehTestRegistry(t, "home")

	ReconcileGfeh(gfehReconcileCtx(t), reg)

	rendered, err := os.ReadFile(gfeh.ConfigPath(base, "home"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(rendered), "smb:") {
		t.Errorf("an SMB view was configured:\n%s", rendered)
	}
	// ... and no credential table of any kind reached the file.
	if strings.Contains(string(rendered), "nt_hash") {
		t.Errorf("an NT hash reached the rendered config:\n%s", rendered)
	}
}

// --- Nothing seats anybody: object storage creates no account ---

// The reconcile used to create a principal named for the box's earliest account
// every time it ran -- an object-storage user nobody asked for, in the home
// partition, appearing in the Users tab as if somebody had added it. It is
// gone, along with the account manager the reconcile needed in order to look
// that account up.
//
// These two tests are shaped to fail if it comes back by any route, rather than
// asserting the absence of one deleted function: one pins the only place in the
// package allowed to create a principal, the other pins the reconcile's inputs
// so it cannot reach the account table at all.

// The only code that creates a principal is the handler an administrator calls.
//
// A source-level assertion because the guarantee is a negative one about the
// whole package: a reconcile, a boot path, or a poller that started creating
// principals would satisfy every behavioural test in this file while
// reintroducing exactly what was removed. Re-seating logic added anywhere but
// the handler fails here.
func TestOnlyTheAdminHandlerCreatesPrincipals(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var callers []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "CreatePrincipal" {
				return true
			}
			callers = append(callers, filepath.Base(name))
			return true
		})
	}
	sort.Strings(callers)

	want := []string{"controller_gfeh.go"}
	if !slices.Equal(callers, want) {
		t.Errorf("CreatePrincipal is called from %v, want only %v.\n"+
			"Object storage must create no user on its own: a partition's forest "+
			"is populated by an administrator over /gfeh/principals/add and by "+
			"nothing else.", callers, want)
	}
}

// The reconcile cannot read the box's accounts, because it is not given them.
//
// Deleting the seating call while leaving the account manager wired in would
// leave the next person one line away from restoring it, and the compiler with
// nothing to say about it. Removing the field is what makes re-adding it a
// deliberate act.
func TestReconcileGfehConfigCarriesNoAccountManager(t *testing.T) {
	cfg := reflect.TypeFor[ReconcileGfehConfig]()
	accountMgr := reflect.TypeFor[account.Manager]()

	for i := range cfg.NumField() {
		f := cfg.Field(i)
		if f.Type == accountMgr {
			t.Errorf("ReconcileGfehConfig.%s is an account.Manager: the gfeh "+
				"reconcile has no business reading the box's accounts, because "+
				"it creates no object-storage user for any of them", f.Name)
		}
	}
}
