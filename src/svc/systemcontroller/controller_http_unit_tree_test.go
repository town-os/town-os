// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// Each test below uses the same mock harness as the flat /systemd/units tests.
// Fixture shape: a parent "gitea" with a dep "postgres", mirroring the canonical
// dep example in the spec (gitea --dep--> postgres, postgres exposes :5432).

func installedGiteaTree(t *testing.T) (*SystemdClient, *systemd.MockManager, *packages.MockInstallManager) {
	t.Helper()
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "core", Name: "gitea", Version: "1.0"},
		{Repo: "core", Name: "gitea--dep--postgres", Version: "15.0"},
	}
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-gitea-1.0.service", ActiveState: "active", Description: "Gitea"},
		{Name: "town-os-package--core-gitea--dep--postgres-15.0.service", ActiveState: "active", Description: "Postgres"},
	}
	inst.Dependencies["core/gitea"] = map[string]packages.DependencyRecord{
		"db": {EffectiveName: "gitea--dep--postgres", Package: "postgres", Repo: "core", Version: "15.0"},
	}
	return c, sd, inst
}

func TestHTTPListUnitsTreeRootsAndChildren(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	out, err := c.ListUnitsTree(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 root, got %d", len(out.Entries))
	}
	root := out.Entries[0]
	if root.PackageIdentifier != "core/gitea@1.0" {
		t.Fatalf("root package_identifier = %q, want core/gitea@1.0", root.PackageIdentifier)
	}
	if root.Repo != "core" || root.Name != "gitea" || root.Version != "1.0" {
		t.Fatalf("root identity triple: repo=%q name=%q version=%q", root.Repo, root.Name, root.Version)
	}
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(root.Children))
	}
	dep := root.Children[0]
	if dep.PackageIdentifier != "core/gitea--dep--postgres@15.0" {
		t.Fatalf("dep package_identifier = %q", dep.PackageIdentifier)
	}
	if dep.DisplayIdentifier != "core/gitea/postgres@15.0" {
		t.Fatalf("dep display_identifier = %q", dep.DisplayIdentifier)
	}
	if !dep.IsDependency {
		t.Error("dep must be flagged IsDependency")
	}
	if len(dep.Children) != 0 {
		t.Errorf("leaf dep must have no children, got %d", len(dep.Children))
	}
}

func TestHTTPListUnitsTreeDependencyNotPromotedToRoot(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	out, err := c.ListUnitsTree(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	for _, r := range out.Entries {
		if r.IsDependency {
			t.Fatalf("dep %q surfaced as a root in tree response", r.PackageIdentifier)
		}
	}
}

func TestHTTPListUnitsTreeNestedDependencies(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// parent → db → backup (three-level dep chain).
	inst.Installed = []packages.PackageIdentity{
		{Repo: "core", Name: "app", Version: "1.0"},
		{Repo: "core", Name: "app--dep--db", Version: "15.0"},
		{Repo: "core", Name: "app--dep--db--dep--backup", Version: "2.0"},
	}
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-app-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-app--dep--db-15.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-app--dep--db--dep--backup-2.0.service", ActiveState: "active"},
	}
	inst.Dependencies["core/app"] = map[string]packages.DependencyRecord{
		"db": {EffectiveName: "app--dep--db", Package: "db", Repo: "core", Version: "15.0"},
	}
	inst.Dependencies["core/app--dep--db"] = map[string]packages.DependencyRecord{
		"backup": {EffectiveName: "app--dep--db--dep--backup", Package: "backup", Repo: "core", Version: "2.0"},
	}

	out, err := c.ListUnitsTree(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 root, got %d", len(out.Entries))
	}
	root := out.Entries[0]
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child of root, got %d", len(root.Children))
	}
	child := root.Children[0]
	if len(child.Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(child.Children))
	}
	grand := child.Children[0]
	if grand.PackageIdentifier != "core/app--dep--db--dep--backup@2.0" {
		t.Fatalf("grandchild id = %q", grand.PackageIdentifier)
	}
}

func TestHTTPListUnitsTreeSkipsMissingDepUnit(t *testing.T) {
	// Dep record present but no systemd unit for it — e.g. the unit was
	// manually removed but the install record wasn't. The tree endpoint
	// must still render the root and its surviving children.
	c, sd, inst := installedGiteaTree(t)
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-gitea-1.0.service", ActiveState: "active"},
		// No postgres unit.
	}
	// Also drop the dep from inst.Installed so the dep's identifier is
	// not in byIdentity.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "core", Name: "gitea", Version: "1.0"},
	}

	out, err := c.ListUnitsTree(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 root, got %d", len(out.Entries))
	}
	if len(out.Entries[0].Children) != 0 {
		t.Fatalf("expected 0 children when dep unit missing, got %d", len(out.Entries[0].Children))
	}
}

func TestHTTPListUnitsTreeStandalonePackage(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
	}

	out, err := c.ListUnitsTree(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 root, got %d", len(out.Entries))
	}
	if len(out.Entries[0].Children) != 0 {
		t.Fatalf("standalone package must have no children, got %d", len(out.Entries[0].Children))
	}
}

func TestHTTPListUnitsTreeNCFailurePropagates(t *testing.T) {
	c, sd, _ := installedGiteaTree(t)
	// Mark the dep's NC as failed — the tree should flag the dep node
	// (not the parent) with NCFailed + ActiveState=failed.
	sd.Units = append(sd.Units,
		systemd.UnitStatus{Name: "town-os-package--core-gitea--dep--postgres-15.0-network.service", ActiveState: "failed"},
	)

	out, err := c.ListUnitsTree(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	root := out.Entries[0]
	if root.NCFailed {
		t.Error("parent NCFailed must not be set by dep NC failure")
	}
	dep := root.Children[0]
	if !dep.NCFailed {
		t.Error("dep NCFailed must propagate from -network.service")
	}
	if dep.ActiveState != "failed" {
		t.Errorf("dep ActiveState = %q, want failed after NC failure", dep.ActiveState)
	}
}

func TestHTTPListUnitsTreePaginationOnRoots(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Three roots, each with one dep. Pagination must count roots only.
	for _, root := range []string{"a", "b", "c"} {
		inst.Installed = append(inst.Installed,
			packages.PackageIdentity{Repo: "repo", Name: root, Version: "1.0"},
			packages.PackageIdentity{Repo: "repo", Name: root + "--dep--child", Version: "1.0"},
		)
		sd.Units = append(sd.Units,
			systemd.UnitStatus{Name: "town-os-package--repo-" + root + "-1.0.service", ActiveState: "active"},
			systemd.UnitStatus{Name: "town-os-package--repo-" + root + "--dep--child-1.0.service", ActiveState: "active"},
		)
		inst.Dependencies["repo/"+root] = map[string]packages.DependencyRecord{
			"child": {EffectiveName: root + "--dep--child", Package: "child", Repo: "repo", Version: "1.0"},
		}
	}

	out, err := c.ListUnitsTree(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("expected 2 roots on first page, got %d", len(out.Entries))
	}
	if !out.HasMore {
		t.Error("expected has_more=true")
	}
	if out.TotalCount != 3 {
		t.Errorf("expected total_count=3 (roots only), got %d", out.TotalCount)
	}
	// Every root on the page must still ship its full child subtree.
	for _, r := range out.Entries {
		if len(r.Children) != 1 {
			t.Errorf("root %q lost its dep under pagination", r.PackageIdentifier)
		}
	}
}

func TestHTTPListUnitsTreeSearchMatchesDescendants(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	// Search term matches the dep's display identifier, not the root.
	out, err := c.ListUnitsTree(context.TODO(), ListParams{Search: "postgres"})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 matching tree (root kept because dep matched), got %d", len(out.Entries))
	}
	if out.Entries[0].PackageIdentifier != "core/gitea@1.0" {
		t.Fatalf("expected root core/gitea, got %q", out.Entries[0].PackageIdentifier)
	}
}

func TestHTTPListUnitsTreeSearchNoMatch(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	out, err := c.ListUnitsTree(context.TODO(), ListParams{Search: "nosuchthing"})
	if err != nil {
		t.Fatalf("ListUnitsTree: %v", err)
	}
	if len(out.Entries) != 0 {
		t.Fatalf("expected 0 entries for unmatched search, got %d", len(out.Entries))
	}
}

// --- Cascade action tests ---

func TestHTTPSetUnitStatusTreeStartIssuesLeavesFirst(t *testing.T) {
	c, sd, _ := installedGiteaTree(t)

	if err := c.SetUnitStatusTree(context.TODO(), "core", "gitea", "1.0", systemd.Start); err != nil {
		t.Fatalf("SetUnitStatusTree(start): %v", err)
	}

	calls := filterSetStatusCalls(sd.GetCalls())
	if len(calls) != 2 {
		t.Fatalf("expected 2 SetStatus calls, got %d: %+v", len(calls), calls)
	}
	// Leaves first: the dep unit must be started before the parent.
	if calls[0].Args[0] != "town-os-package--core-gitea--dep--postgres-15.0.service" {
		t.Errorf("start: first call = %v, want dep unit first", calls[0].Args[0])
	}
	if calls[1].Args[0] != "town-os-package--core-gitea-1.0.service" {
		t.Errorf("start: second call = %v, want parent unit last", calls[1].Args[0])
	}
	for _, call := range calls {
		if call.Args[1] != systemd.Start {
			t.Errorf("expected action=start, got %v", call.Args[1])
		}
	}
}

func TestHTTPSetUnitStatusTreeStopIssuesParentFirst(t *testing.T) {
	c, sd, _ := installedGiteaTree(t)

	if err := c.SetUnitStatusTree(context.TODO(), "core", "gitea", "1.0", systemd.Stop); err != nil {
		t.Fatalf("SetUnitStatusTree(stop): %v", err)
	}

	calls := filterSetStatusCalls(sd.GetCalls())
	if len(calls) != 2 {
		t.Fatalf("expected 2 SetStatus calls, got %d", len(calls))
	}
	if calls[0].Args[0] != "town-os-package--core-gitea-1.0.service" {
		t.Errorf("stop: first call = %v, want parent unit first", calls[0].Args[0])
	}
	if calls[1].Args[0] != "town-os-package--core-gitea--dep--postgres-15.0.service" {
		t.Errorf("stop: second call = %v, want dep unit last", calls[1].Args[0])
	}
}

func TestHTTPSetUnitStatusTreeRestartHitsEveryNode(t *testing.T) {
	c, sd, _ := installedGiteaTree(t)

	if err := c.SetUnitStatusTree(context.TODO(), "core", "gitea", "1.0", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatusTree(restart): %v", err)
	}

	calls := filterSetStatusCalls(sd.GetCalls())
	if len(calls) != 2 {
		t.Fatalf("expected 2 SetStatus calls, got %d", len(calls))
	}
	for _, call := range calls {
		if call.Args[1] != systemd.Restart {
			t.Errorf("expected action=restart, got %v", call.Args[1])
		}
	}
}

func TestHTTPSetUnitStatusTreeEnableRejected(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	err := c.SetUnitStatusTree(context.TODO(), "core", "gitea", "1.0", systemd.Enable)
	if err == nil {
		t.Fatal("expected enable to be rejected for tree cascade")
	}
}

func TestHTTPSetUnitStatusTreeDisableRejected(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	err := c.SetUnitStatusTree(context.TODO(), "core", "gitea", "1.0", systemd.Disable)
	if err == nil {
		t.Fatal("expected disable to be rejected for tree cascade")
	}
}

func TestHTTPSetUnitStatusTreeRequiresAllIdentityFields(t *testing.T) {
	c, _, _ := installedGiteaTree(t)

	for _, m := range []struct {
		repo, name, version string
	}{
		{"", "gitea", "1.0"},
		{"core", "", "1.0"},
		{"core", "gitea", ""},
	} {
		if err := c.SetUnitStatusTree(context.TODO(), m.repo, m.name, m.version, systemd.Start); err == nil {
			t.Errorf("expected error for missing identity field: %+v", m)
		}
	}
}

func TestHTTPSetUnitStatusTreeNestedThreeLevels(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	inst.Installed = []packages.PackageIdentity{
		{Repo: "core", Name: "app", Version: "1.0"},
		{Repo: "core", Name: "app--dep--db", Version: "15.0"},
		{Repo: "core", Name: "app--dep--db--dep--backup", Version: "2.0"},
	}
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--core-app-1.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-app--dep--db-15.0.service", ActiveState: "active"},
		{Name: "town-os-package--core-app--dep--db--dep--backup-2.0.service", ActiveState: "active"},
	}
	inst.Dependencies["core/app"] = map[string]packages.DependencyRecord{
		"db": {EffectiveName: "app--dep--db", Package: "db", Repo: "core", Version: "15.0"},
	}
	inst.Dependencies["core/app--dep--db"] = map[string]packages.DependencyRecord{
		"backup": {EffectiveName: "app--dep--db--dep--backup", Package: "backup", Repo: "core", Version: "2.0"},
	}

	if err := c.SetUnitStatusTree(context.TODO(), "core", "app", "1.0", systemd.Start); err != nil {
		t.Fatalf("SetUnitStatusTree(start): %v", err)
	}

	calls := filterSetStatusCalls(sd.GetCalls())
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls for 3-level chain, got %d", len(calls))
	}
	// Start order is leaves-first: backup, then db, then app.
	wantOrder := []string{
		"town-os-package--core-app--dep--db--dep--backup-2.0.service",
		"town-os-package--core-app--dep--db-15.0.service",
		"town-os-package--core-app-1.0.service",
	}
	for i, w := range wantOrder {
		if calls[i].Args[0] != w {
			t.Errorf("start call %d = %v, want %s", i, calls[i].Args[0], w)
		}
	}
}

func TestHTTPSetUnitStatusTreeSystemdErrorPropagates(t *testing.T) {
	c, sd, _ := installedGiteaTree(t)
	sd.StatusErr = errors.New("injected")

	if err := c.SetUnitStatusTree(context.TODO(), "core", "gitea", "1.0", systemd.Start); err == nil {
		t.Fatal("expected SetUnitStatusTree to propagate systemd error")
	}
}

func TestHTTPSetUnitStatusTreeStandalonePackage(t *testing.T) {
	c, sd, inst := initSystemdTestClient(t)

	// Standalone package (no deps) must still cascade — just the one unit.
	inst.Installed = []packages.PackageIdentity{
		{Repo: "repo", Name: "nginx", Version: "1.0"},
	}
	sd.Units = []systemd.UnitStatus{
		{Name: "town-os-package--repo-nginx-1.0.service", ActiveState: "active"},
	}

	if err := c.SetUnitStatusTree(context.TODO(), "repo", "nginx", "1.0", systemd.Restart); err != nil {
		t.Fatalf("SetUnitStatusTree(restart): %v", err)
	}
	calls := filterSetStatusCalls(sd.GetCalls())
	if len(calls) != 1 {
		t.Fatalf("expected 1 call for standalone package, got %d", len(calls))
	}
	if calls[0].Args[0] != "town-os-package--repo-nginx-1.0.service" {
		t.Errorf("unit = %v", calls[0].Args[0])
	}
}

func filterSetStatusCalls(calls []systemd.MockCall) []systemd.MockCall {
	out := []systemd.MockCall{}
	for _, c := range calls {
		if c.Method == "SetStatus" {
			out = append(out, c)
		}
	}
	return out
}
