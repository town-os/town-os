// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// initCascadeTestClient wires a SystemdClient against mock storage, a mock
// install manager, and a mock systemd manager so the cascade-delete
// handler can exercise both sides of the contract (unit stops + subvol
// removals) without a real systemd/btrfs.
func initCascadeTestClient(t *testing.T) (*SystemdClient, *storage.MockBtrFSController, *packages.MockInstallManager, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	controller, ok := mock.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("type assertion failed")
	}
	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{Storage: mock, Installer: inst, Systemd: sd})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, controller, inst, sd
}

// stopCalls returns the unit names the mock systemd manager observed a Stop
// action for, in the order they were issued.
func stopCalls(sd *systemd.MockManager) []string {
	var out []string
	for _, c := range sd.GetCalls() {
		if c.Method != "SetStatus" || len(c.Args) != 2 {
			continue
		}
		unit, ok := c.Args[0].(string)
		if !ok {
			continue
		}
		action, ok := c.Args[1].(systemd.StatusAction)
		if !ok {
			continue
		}
		if action != systemd.Stop {
			continue
		}
		out = append(out, unit)
	}
	return out
}

// subvolExists checks whether the mock btrfs controller still has the named
// subvolume registered.
func subvolExists(ctrl *storage.MockBtrFSController, name string) bool {
	for _, fs := range ctrl.GetFilesystems() {
		if fs.Name == name {
			return true
		}
	}
	return false
}

// seedInstalled registers an install record for (repo, name@version) on
// the mock install manager. Version-only group deletes need the install
// manager to have a record so stopVolumeGroupUnits can find it.
func seedInstalled(t *testing.T, inst *packages.MockInstallManager, repo, name, version string) { //nolint:unparam // repo varies in future tests; keep the shape explicit
	t.Helper()
	if err := inst.Install(repo, name, name, version, packages.Responses{}); err != nil {
		t.Fatalf("seedInstalled %s/%s@%s: %v", repo, name, version, err)
	}
}

func TestRemovePackageVolumeGroup_PackageLevel(t *testing.T) {
	c, ctrl, inst, sd := initCascadeTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/config", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/2.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-a/other/1.0/data", 0)
	seedInstalled(t, inst, "repo-a", "nginx", "1.0")
	seedInstalled(t, inst, "repo-a", "nginx", "2.0")
	seedInstalled(t, inst, "repo-a", "other", "1.0")

	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "nginx", "", false); err != nil {
		t.Fatalf("RemovePackageVolumeGroup: %v", err)
	}

	// Every nginx volume (both versions) is gone.
	for _, name := range []string{
		"installed/repo-a/nginx/1.0/data",
		"installed/repo-a/nginx/1.0/config",
		"installed/repo-a/nginx/2.0/data",
	} {
		if subvolExists(ctrl, name) {
			t.Errorf("%q still present after cascade delete", name)
		}
	}
	// Sibling package untouched.
	if !subvolExists(ctrl, "installed/repo-a/other/1.0/data") {
		t.Errorf("unrelated package volume was removed")
	}

	stops := stopCalls(sd)
	if len(stops) != 2 {
		t.Fatalf("expected 2 Stop calls (one per installed version), got %d: %v", len(stops), stops)
	}
	wantStops := map[string]bool{
		systemd.UnitName("repo-a", "nginx", "1.0"): false,
		systemd.UnitName("repo-a", "nginx", "2.0"): false,
	}
	for _, u := range stops {
		if _, ok := wantStops[u]; !ok {
			t.Errorf("unexpected unit stopped: %s", u)
			continue
		}
		wantStops[u] = true
	}
	for u, hit := range wantStops {
		if !hit {
			t.Errorf("expected unit %s to be stopped", u)
		}
	}
}

func TestRemovePackageVolumeGroup_VersionLevel(t *testing.T) {
	c, ctrl, inst, sd := initCascadeTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/config", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/2.0/data", 0)
	seedInstalled(t, inst, "repo-a", "nginx", "1.0")
	seedInstalled(t, inst, "repo-a", "nginx", "2.0")

	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("RemovePackageVolumeGroup: %v", err)
	}

	// 1.0 volumes gone; 2.0 untouched.
	if subvolExists(ctrl, "installed/repo-a/nginx/1.0/data") {
		t.Errorf("1.0/data still present")
	}
	if subvolExists(ctrl, "installed/repo-a/nginx/1.0/config") {
		t.Errorf("1.0/config still present")
	}
	if !subvolExists(ctrl, "installed/repo-a/nginx/2.0/data") {
		t.Errorf("2.0/data was removed by a version-scoped cascade")
	}

	// Only the 1.0 unit should have been stopped — the install manager
	// knows about 2.0 too, but a version-scoped cascade must leave it
	// alone.
	stops := stopCalls(sd)
	if len(stops) != 1 {
		t.Fatalf("expected 1 Stop call, got %d: %v", len(stops), stops)
	}
	if stops[0] != systemd.UnitName("repo-a", "nginx", "1.0") {
		t.Errorf("expected %q stopped, got %q", systemd.UnitName("repo-a", "nginx", "1.0"), stops[0])
	}
}

func TestRemovePackageVolumeGroup_CascadesDependencyUnits(t *testing.T) {
	c, ctrl, inst, sd := initCascadeTestClient(t)

	// Parent + nested dep layout on disk.
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/subpackages/db/1.0/data", 0)
	seedInstalled(t, inst, "repo-a", "nginx", "1.0")
	seedInstalled(t, inst, "repo-a", "nginx--dep--db", "1.0")
	if err := inst.SaveDependencies("repo-a", "nginx", map[string]packages.DependencyRecord{
		"db": {EffectiveName: "nginx--dep--db", Package: "db", Repo: "repo-a", Version: "1.0"},
	}); err != nil {
		t.Fatalf("SaveDependencies: %v", err)
	}

	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("RemovePackageVolumeGroup: %v", err)
	}

	// Dep subvolume is gone too (purgeVolumePrefix sweeps nested
	// subpackages/...).
	if subvolExists(ctrl, "installed/repo-a/nginx/subpackages/db/1.0/data") {
		t.Errorf("dep subvolume still present after cascade")
	}

	// Both the parent unit and the dep unit should have been stopped, in
	// that order — parent first so PartOf cascades land before the
	// belt-and-braces dep stop.
	stops := stopCalls(sd)
	wantParent := systemd.UnitName("repo-a", "nginx", "1.0")
	wantDep := systemd.UnitName("repo-a", "nginx--dep--db", "1.0")
	if len(stops) != 2 {
		t.Fatalf("expected 2 Stop calls, got %d: %v", len(stops), stops)
	}
	if stops[0] != wantParent {
		t.Errorf("expected parent %q stopped first, got %q", wantParent, stops[0])
	}
	if stops[1] != wantDep {
		t.Errorf("expected dep %q stopped second, got %q", wantDep, stops[1])
	}
}

func TestRemovePackageVolumeGroup_IncludeUninstalled(t *testing.T) {
	c, ctrl, _, _ := initCascadeTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "uninstalled/repo-a/nginx/0.9/data", 0)

	// Without the flag, uninstalled is left behind even on a package-level
	// cascade.
	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "nginx", "", false); err != nil {
		t.Fatalf("RemovePackageVolumeGroup (exclude): %v", err)
	}
	if subvolExists(ctrl, "installed/repo-a/nginx/1.0/data") {
		t.Errorf("installed volume still present")
	}
	if !subvolExists(ctrl, "uninstalled/repo-a/nginx/0.9/data") {
		t.Errorf("uninstalled volume removed despite include_uninstalled=false")
	}

	// With the flag set, the uninstalled subtree is swept too.
	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "nginx", "", true); err != nil {
		t.Fatalf("RemovePackageVolumeGroup (include): %v", err)
	}
	if subvolExists(ctrl, "uninstalled/repo-a/nginx/0.9/data") {
		t.Errorf("uninstalled volume still present after include_uninstalled=true cascade")
	}
}

func TestRemovePackageVolumeGroup_RejectsMissingFields(t *testing.T) {
	c, _, _, _ := initCascadeTestClient(t) //nolint:dogsled // storage/installer/systemd not needed for this case

	if err := c.RemovePackageVolumeGroup(context.TODO(), "", "nginx", "1.0", false); err == nil {
		t.Error("expected error for empty repo, got nil")
	}
	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "", "1.0", false); err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestRemovePackageVolumeGroup_NoInstallRecordIsNoop(t *testing.T) {
	c, ctrl, _, sd := initCascadeTestClient(t)

	// No install record, no subvolumes matching the prefix. The handler
	// should succeed (idempotent) without emitting any Stop calls.
	if err := c.RemovePackageVolumeGroup(context.TODO(), "repo-a", "nginx", "", false); err != nil {
		t.Fatalf("RemovePackageVolumeGroup: %v", err)
	}
	if got := stopCalls(sd); len(got) != 0 {
		t.Errorf("expected no Stop calls, got %v", got)
	}
	// And nothing spurious got created either.
	for _, fs := range ctrl.GetFilesystems() {
		if strings.HasPrefix(fs.Name, "installed/repo-a/nginx") {
			t.Errorf("unexpected residual subvolume %q", fs.Name)
		}
	}
}
