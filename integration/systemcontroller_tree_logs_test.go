// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestSystemControllerTreeLogsExpandDependencies exercises the
// /systemd/logs/tree/tail endpoint end-to-end through a real install of
// a package with a dependency.
//
// The install path creates both the parent and dep systemd units (against
// the mock systemd manager). The tree log endpoint then resolves the
// parent's dependency records, emits the full unit list as a `Units`
// filter on LogTailParams, and the mock's recorded call lets us assert
// the expected unit names without a real journal.
func TestSystemControllerTreeLogsExpandDependencies(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9091",
		"cachepass": "treelogsecret",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage app-with-cache@1.0: %v", err)
	}

	// Seed two journal entries so we can observe the endpoint delivering
	// real data on top of the unit-expansion assertion.
	sd.Entries = []systemd.JournalEntry{
		{Cursor: "c1", Message: "parent boot", RealtimeTimestamp: time.Now().Add(-2 * time.Second)},
		{Cursor: "c2", Message: "dep ready", RealtimeTimestamp: time.Now().Add(-time.Second)},
	}

	result, err := c.LogTailTree(context.TODO(), "core", "app-with-cache", "1.0", systemd.LogTailParams{Lines: 100})
	if err != nil {
		t.Fatalf("LogTailTree: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries from tree tail, got %d", len(result.Entries))
	}

	// Confirm the handler expanded the tree to both the parent and the
	// dep's effective unit names. Leaves-first ordering is a consequence
	// of collectTreeUnits which is shared with the status-tree cascade;
	// asserting on the sorted set keeps the test resilient if the
	// cascade walk order is ever tweaked independently.
	depEff := packages.DependencyName("app-with-cache", "cache")
	wantUnits := []string{
		systemd.UnitName("core", depEff, "7.0"),
		systemd.UnitName("core", "app-with-cache", "1.0"),
	}
	sort.Strings(wantUnits)

	var recorded systemd.LogTailParams
	found := false
	for _, call := range sd.GetCalls() {
		if call.Method != "LogTail" {
			continue
		}
		p, ok := call.Args[0].(systemd.LogTailParams)
		if !ok {
			continue
		}
		recorded = p
		found = true
	}
	if !found {
		t.Fatal("no LogTail call recorded on mock systemd")
	}

	gotUnits := append([]string(nil), recorded.Units...)
	sort.Strings(gotUnits)
	if !reflect.DeepEqual(gotUnits, wantUnits) {
		t.Fatalf("recorded Units = %v, want %v", gotUnits, wantUnits)
	}
}

// TestSystemControllerTreeLogsReplayExpandsDependencies mirrors the tail
// test but exercises the SSE replay endpoint. The replay handler shares
// collectTreeUnits with the tail handler but records on the mock via
// LogReplay (with a []string args entry when multiple units are
// forwarded), so pinning the recorded shape protects the SSE path
// independently.
func TestSystemControllerTreeLogsReplayExpandsDependencies(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9092",
		"cachepass": "replaysecret",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage app-with-cache@1.0: %v", err)
	}

	sd.Entries = []systemd.JournalEntry{
		{Message: "replay entry 1"},
		{Message: "replay entry 2"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := c.LogReplayTree(ctx, "core", "app-with-cache", "1.0")
	if err != nil {
		t.Fatalf("LogReplayTree: %v", err)
	}

	var got []systemd.JournalEntry
	for e := range ch {
		got = append(got, e)
		if len(got) >= 2 {
			cancel()
			break
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected at least 2 entries from replay stream, got %d", len(got))
	}

	depEff := packages.DependencyName("app-with-cache", "cache")
	wantUnits := []string{
		systemd.UnitName("core", depEff, "7.0"),
		systemd.UnitName("core", "app-with-cache", "1.0"),
	}
	sort.Strings(wantUnits)

	var recordedUnits []string
	for _, call := range sd.GetCalls() {
		if call.Method != "LogReplay" {
			continue
		}
		v, ok := call.Args[0].([]string)
		if !ok {
			continue
		}
		recordedUnits = append([]string(nil), v...)
	}
	sort.Strings(recordedUnits)
	if !reflect.DeepEqual(recordedUnits, wantUnits) {
		t.Fatalf("recorded replay Units = %v, want %v", recordedUnits, wantUnits)
	}
}

// TestSystemControllerStatusTreeCascadeThroughClient exercises the
// /systemd/status/tree endpoint end-to-end. The cascade is already
// covered by HTTP tests, but this integration variant verifies the
// full pipeline through the real Go client and the mock systemd
// manager, matching the coverage shape used for the volume-group
// cascade.
func TestSystemControllerStatusTreeCascadeThroughClient(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9093",
		"cachepass": "cascadesecret",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage app-with-cache@1.0: %v", err)
	}

	// Flush the install-time SetStatus calls so the cascade assertions
	// only see the ones we're about to issue.
	sd.ClearCalls()

	if err := c.SetUnitStatusTree(context.TODO(), "core", "app-with-cache", "1.0", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatusTree(restart): %v", err)
	}

	parentUnit := systemd.UnitName("core", "app-with-cache", "1.0")
	depUnit := systemd.UnitName("core", packages.DependencyName("app-with-cache", "cache"), "7.0")

	// Both units must have been restarted exactly once. Cascade order
	// is tested against the HTTP layer in controller_http_unit_tree_test.go;
	// the integration test only pins the full set so a regression in
	// dep-record lookup or action routing surfaces here.
	wanted := map[string]bool{parentUnit: false, depUnit: false}
	for _, call := range sd.GetCalls() {
		if call.Method != "SetStatus" {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok {
			continue
		}
		action, ok := call.Args[1].(systemd.StatusAction)
		if !ok {
			continue
		}
		if action != systemd.Restart {
			continue
		}
		if _, tracked := wanted[name]; tracked {
			wanted[name] = true
		}
	}
	for name, saw := range wanted {
		if !saw {
			t.Errorf("cascade did not Restart %s", name)
		}
	}
}
