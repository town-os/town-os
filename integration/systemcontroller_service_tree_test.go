// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestServiceTreeGroupsParentAndDependencies verifies that after installing
// a package with a dependency, the /systemd/units-tree endpoint groups the
// parent's service unit and the dep's service unit into a single tree with
// the dep nested under the parent. This is the observable end-to-end proof
// that grouped services work: a user clicking through the UI would see the
// parent package with its deps under it instead of a flat row list.
func TestServiceTreeGroupsParentAndDependencies(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9090",
		"cachepass": "secret123",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage app-with-cache@1.0: %v", err)
	}

	tree, err := c.ListUnitsTree(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}

	if len(tree.Entries) != 1 {
		t.Fatalf("expected 1 root (the parent), got %d: %+v", len(tree.Entries), tree.Entries)
	}
	root := tree.Entries[0]
	if root.Repo != "core" || root.Name != "app-with-cache" || root.Version != "1.0" {
		t.Fatalf("root identity triple wrong: repo=%q name=%q version=%q", root.Repo, root.Name, root.Version)
	}
	if root.IsDependency {
		t.Error("parent must not be flagged as dependency")
	}
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 dep child, got %d", len(root.Children))
	}
	dep := root.Children[0]
	if dep.Name != packages.DependencyName("app-with-cache", "cache") {
		t.Fatalf("dep effective name = %q, want %q", dep.Name, packages.DependencyName("app-with-cache", "cache"))
	}
	if !dep.IsDependency {
		t.Error("dep child must be flagged IsDependency")
	}
	if dep.DisplayIdentifier != "core/app-with-cache/cache@7.0" {
		t.Errorf("dep display_identifier = %q, want core/app-with-cache/cache@7.0", dep.DisplayIdentifier)
	}

	// Uninstalling the parent must empty the tree (cascade removes the dep).
	if err := c.UninstallPackage(context.TODO(), "core", "app-with-cache", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}
	post, err := c.ListUnitsTree(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree post: %v", err)
	}
	if len(post.Entries) != 0 {
		t.Fatalf("expected empty tree after uninstall, got %d: %+v", len(post.Entries), post.Entries)
	}
}

// TestServiceTreeCascadeActionHitsParentAndDependency verifies that
// POST /systemd/status/tree cascades the action across the whole dependency
// tree. After installing a parent with a dep, issuing a single "stop"
// cascade must produce SetStatus calls for both the parent and its dep
// unit, in parent-first (post-order-reversed) order so PartOf= has time
// to propagate before the explicit dep stop.
func TestServiceTreeCascadeActionHitsParentAndDependency(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9090",
		"cachepass": "secret123",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Clear the mock's call history so SetStatus calls issued by the
	// cascade are the only ones we examine. InstallPackage itself makes
	// several unrelated systemd calls (InstallUnit, Start, Reload, …).
	preCascadeCalls := len(sd.GetCalls())

	if err := c.SetUnitStatusTree(context.TODO(), "core", "app-with-cache", "1.0", systemd.Stop); err != nil {
		t.Fatalf("SetUnitStatusTree(stop): %v", err)
	}

	// Collect only the SetStatus calls that happened after the install
	// finished.
	parentUnit := systemd.UnitName("core", "app-with-cache", "1.0")
	depEffName := packages.DependencyName("app-with-cache", "cache")
	depUnit := systemd.UnitName("core", depEffName, "7.0")

	allCalls := sd.GetCalls()
	if len(allCalls) <= preCascadeCalls {
		t.Fatalf("expected new systemd calls after cascade, got %d total", len(allCalls))
	}
	postCalls := allCalls[preCascadeCalls:]

	var setStatusUnits []string
	for _, call := range postCalls {
		if call.Method != "SetStatus" {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok {
			t.Fatal("type assertion failed on SetStatus arg 0")
		}
		action, ok := call.Args[1].(systemd.StatusAction)
		if !ok {
			t.Fatal("type assertion failed on SetStatus arg 1")
		}
		if action != systemd.Stop {
			t.Errorf("cascade call used action=%v, want stop", action)
		}
		setStatusUnits = append(setStatusUnits, name)
	}

	if len(setStatusUnits) != 2 {
		t.Fatalf("expected 2 SetStatus calls (parent + dep), got %d: %v", len(setStatusUnits), setStatusUnits)
	}

	// Stop order is parent-first, then dep. This lets systemd's PartOf=
	// fire before the explicit dep stop; the explicit call is a fallback.
	if setStatusUnits[0] != parentUnit {
		t.Errorf("stop order: first = %q, want parent %q", setStatusUnits[0], parentUnit)
	}
	if setStatusUnits[1] != depUnit {
		t.Errorf("stop order: second = %q, want dep %q", setStatusUnits[1], depUnit)
	}
}

// TestServiceTreeCascadeStartLeavesFirst is the start-side mirror of
// TestServiceTreeCascadeActionHitsParentAndDependency: a start cascade
// must emit the dep unit *before* the parent so After=/Wants= see the
// dep already running by the time the parent transaction runs.
func TestServiceTreeCascadeStartLeavesFirst(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9090",
		"cachepass": "secret123",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	preCascadeCalls := len(sd.GetCalls())

	if err := c.SetUnitStatusTree(context.TODO(), "core", "app-with-cache", "1.0", systemd.Start); err != nil {
		t.Fatalf("SetUnitStatusTree(start): %v", err)
	}

	parentUnit := systemd.UnitName("core", "app-with-cache", "1.0")
	depEffName := packages.DependencyName("app-with-cache", "cache")
	depUnit := systemd.UnitName("core", depEffName, "7.0")

	var setStatusUnits []string
	for _, call := range sd.GetCalls()[preCascadeCalls:] {
		if call.Method != "SetStatus" {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok {
			t.Fatal("type assertion failed")
		}
		setStatusUnits = append(setStatusUnits, name)
	}

	if len(setStatusUnits) != 2 {
		t.Fatalf("expected 2 SetStatus calls, got %d: %v", len(setStatusUnits), setStatusUnits)
	}
	if setStatusUnits[0] != depUnit {
		t.Errorf("start order: first = %q, want dep %q (leaves-first)", setStatusUnits[0], depUnit)
	}
	if setStatusUnits[1] != parentUnit {
		t.Errorf("start order: second = %q, want parent %q", setStatusUnits[1], parentUnit)
	}
}
