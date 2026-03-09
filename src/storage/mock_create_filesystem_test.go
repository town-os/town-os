// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"sort"
	"strings"
	"testing"
)

func TestCreateFilesystemValidatesName(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "/bad"})
	if err == nil {
		t.Fatal("expected error for leading slash")
	}

	err = mock.CreateFilesystem(Filesystem{Name: ".."})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	err = mock.CreateFilesystem(Filesystem{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateFilesystemWithQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 8192})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if controller.Quotas["vol"] != 8192 {
		t.Fatalf("expected quota 8192, got %d", controller.Quotas["vol"])
	}
}

func TestCreateFilesystemNestedPath(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "a/b/c"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	// All 3 subvolumes should exist
	err = controller.IsSubvolume("a")
	if err != nil {
		t.Fatalf("expected intermediate 'a' to exist: %v", err)
	}
	err = controller.IsSubvolume("a/b")
	if err != nil {
		t.Fatalf("expected intermediate 'a/b' to exist: %v", err)
	}
	err = controller.IsSubvolume("a/b/c")
	if err != nil {
		t.Fatalf("expected leaf 'a/b/c' to exist: %v", err)
	}
}

func TestCreateFilesystemNestedPartialExists(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	// Pre-create 'a'
	err := controller.SubvolCreate("a")
	if err != nil {
		t.Fatalf("SubvolCreate a: %v", err)
	}

	err = mock.CreateFilesystem(Filesystem{Name: "a/b/c"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	// Count SubvolCreate calls — 'a' was pre-created, so only 'a/b' and 'a/b/c' should be new
	createCount := 0
	for _, c := range controller.GetLog() {
		if c.Operation == "SubvolCreate" {
			createCount++
		}
	}
	// 1 for pre-create of 'a', 1 for 'a/b', 1 for 'a/b/c' = 3
	if createCount != 3 {
		t.Fatalf("expected 3 SubvolCreate calls (1 pre-create + 2 new), got %d", createCount)
	}
}

func TestCreateFilesystemNestedQuotaOnlyOnLeaf(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "a/b/c", Quota: 1024})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	// Quota should only be on the leaf
	if controller.Quotas["a/b/c"] != 1024 {
		t.Fatalf("expected quota 1024 on leaf, got %d", controller.Quotas["a/b/c"])
	}

	if _, ok := controller.Quotas["a"]; ok {
		t.Fatal("intermediate 'a' should not have quota")
	}

	if _, ok := controller.Quotas["a/b"]; ok {
		t.Fatal("intermediate 'a/b' should not have quota")
	}
}

func TestPurgeSimulation(t *testing.T) {
	mock := InitBtrFSMock()

	// Create a deep hierarchy mimicking a package with nested volumes.
	for _, name := range []string{"pkg/a", "pkg/a/b", "pkg/a/b/c"} {
		err := mock.CreateFilesystem(Filesystem{Name: name})
		if err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Also create a sibling package to verify it survives.
	err := mock.CreateFilesystem(Filesystem{Name: "other/data"})
	if err != nil {
		t.Fatalf("CreateFilesystem other/data: %v", err)
	}

	// Simulate the purge algorithm: list children, sort deepest-first, remove.
	prefix := "pkg/"
	filesystems, err := mock.ListFilesystems(prefix)
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	sort.Slice(filesystems, func(i, j int) bool {
		return strings.Count(filesystems[i].Name, "/") > strings.Count(filesystems[j].Name, "/")
	})

	// Verify deepest-first ordering.
	if len(filesystems) < 2 {
		t.Fatalf("expected at least 2 filesystems, got %d", len(filesystems))
	}
	for i := 1; i < len(filesystems); i++ {
		depthPrev := strings.Count(filesystems[i-1].Name, "/")
		depthCurr := strings.Count(filesystems[i].Name, "/")
		if depthPrev < depthCurr {
			t.Fatalf("sort order wrong: %q (depth %d) before %q (depth %d)",
				filesystems[i-1].Name, depthPrev, filesystems[i].Name, depthCurr)
		}
	}

	// Remove children deepest-first.
	for _, fs := range filesystems {
		err := mock.RemoveFilesystem(fs.Name)
		if err != nil {
			t.Fatalf("RemoveFilesystem %q: %v", fs.Name, err)
		}
	}

	// Remove the parent package volume itself.
	err = mock.RemoveFilesystem("pkg")
	if err != nil {
		t.Fatalf("RemoveFilesystem pkg: %v", err)
	}

	// Verify pkg is completely gone.
	remaining, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	for _, fs := range remaining {
		if fs.Name == "pkg" || strings.HasPrefix(fs.Name, "pkg/") {
			t.Fatalf("expected all pkg volumes purged, found %q", fs.Name)
		}
	}

	// Verify other package survived.
	found := map[string]bool{}
	for _, fs := range remaining {
		found[fs.Name] = true
	}
	if !found["other"] {
		t.Fatal("expected other parent volume to survive purge")
	}
	if !found["other/data"] {
		t.Fatal("expected other/data volume to survive purge")
	}
}
