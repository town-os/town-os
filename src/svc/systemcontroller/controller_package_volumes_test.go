// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"testing"
)

func TestListPackageVolumes_BasicGrouping(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 1024)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/config", 512)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if g.Package != "nginx" {
		t.Fatalf("expected package %q, got %q", "nginx", g.Package)
	}
	if g.Repo != "repo-a" {
		t.Fatalf("expected repo %q, got %q", "repo-a", g.Repo)
	}
	if len(g.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(g.Volumes))
	}

	// Volumes should be sorted by name
	if g.Volumes[0].Name != "1.0/config" {
		t.Fatalf("expected first volume %q, got %q", "1.0/config", g.Volumes[0].Name)
	}
	if g.Volumes[1].Name != "1.0/data" {
		t.Fatalf("expected second volume %q, got %q", "1.0/data", g.Volumes[1].Name)
	}
}

func TestListPackageVolumes_MultipleVersions(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/2.0/data", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if len(groups[0].Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(groups[0].Volumes))
	}

	// Should appear flat under one package
	names := map[string]bool{}
	for _, v := range groups[0].Volumes {
		names[v.Name] = true
	}
	if !names["1.0/data"] || !names["2.0/data"] {
		t.Fatalf("expected volumes 1.0/data and 2.0/data, got %v", names)
	}
}

func TestListPackageVolumes_NameCollision(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-b/nginx/1.0/data", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	names := map[string]bool{}
	for _, g := range groups {
		names[g.Package] = true
	}

	if !names["repo-a/nginx"] || !names["repo-b/nginx"] {
		t.Fatalf("expected repo-prefixed display names, got %v", names)
	}
}

func TestListPackageVolumes_NoCollisionSingleRepo(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if groups[0].Package != "nginx" {
		t.Fatalf("expected display name %q (no repo prefix), got %q", "nginx", groups[0].Package)
	}
}

func TestListPackageVolumes_IncludeUninstalled(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "uninstalled/repo-a/nginx/0.9/data", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), true)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if len(groups[0].Volumes) != 2 {
		t.Fatalf("expected 2 volumes (installed + uninstalled), got %d", len(groups[0].Volumes))
	}

	states := map[string]bool{}
	for _, v := range groups[0].Volumes {
		states[v.State] = true
	}
	if !states["installed"] || !states["uninstalled"] {
		t.Fatalf("expected both installed and uninstalled states, got %v", states)
	}
}

func TestListPackageVolumes_ExcludeUninstalled(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "uninstalled/repo-a/nginx/0.9/data", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if len(groups[0].Volumes) != 1 {
		t.Fatalf("expected 1 volume (only installed), got %d", len(groups[0].Volumes))
	}

	if groups[0].Volumes[0].State != "installed" {
		t.Fatalf("expected installed state, got %q", groups[0].Volumes[0].State)
	}
}

func TestListPackageVolumes_SkipsIntermediateSubvolumes(t *testing.T) {
	c, ctrl := initTestClient(t)

	// Inject intermediate subvolumes (fewer than 4 parts after prefix)
	injectSubvol(t, ctrl, "installed/repo-a/nginx", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0", 0)
	// Inject a real volume
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if len(groups[0].Volumes) != 1 {
		t.Fatalf("expected 1 volume (intermediates skipped), got %d", len(groups[0].Volumes))
	}

	if groups[0].Volumes[0].Name != "1.0/data" {
		t.Fatalf("expected volume name %q, got %q", "1.0/data", groups[0].Volumes[0].Name)
	}
}

func TestListPackageVolumes_RepoInEveryVolume(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)
	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/config", 0)

	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes: %v", err)
	}

	for _, g := range groups {
		if g.Repo == "" {
			t.Fatalf("expected repo to be set on group %q", g.Package)
		}
		for _, v := range g.Volumes {
			if v.Repo == "" {
				t.Fatalf("expected repo to be set on volume %q", v.Name)
			}
		}
	}
}

func TestRemovePackageVolume(t *testing.T) {
	c, ctrl := initTestClient(t)

	injectSubvol(t, ctrl, "installed/repo-a/nginx/1.0/data", 0)

	// Verify it exists
	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes before: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group before removal, got %d", len(groups))
	}

	// Remove it
	if err := c.RemovePackageVolume(context.TODO(), "installed/repo-a/nginx/1.0/data"); err != nil {
		t.Fatalf("RemovePackageVolume: %v", err)
	}

	// Verify it's gone
	groups, err = c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes after: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups after removal, got %d", len(groups))
	}
}

func TestRemovePackageVolume_RejectsUserVolume(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.RemovePackageVolume(context.TODO(), "user/foo")
	if err == nil {
		t.Fatal("expected error when removing user volume via package endpoint, got nil")
	}
}

func TestRemovePackageVolume_RejectsArbitraryPath(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.RemovePackageVolume(context.TODO(), "some/arbitrary/path")
	if err == nil {
		t.Fatal("expected error when removing arbitrary path, got nil")
	}
}
