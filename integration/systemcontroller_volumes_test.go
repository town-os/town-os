package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- Subvolume and quota integration tests ---

func TestSystemControllerCreateWithQuota(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-quota", Quota: 1048576}); err != nil {
		t.Fatalf("CreateFilesystem with quota: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-quota"); err != nil {
			t.Errorf("cleanup RemoveFilesystem: %v", err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), "sc-quota", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Quota != 1048576 {
		t.Fatalf("expected quota %d, got %d", 1048576, listResult.Entries[0].Quota)
	}
}

func TestSystemControllerModifyQuota(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-modq"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-modq"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	// Set quota.
	if err := c.ModifyFilesystem(context.TODO(), "sc-modq", storage.Filesystem{Name: "sc-modq", Quota: 2097152}); err != nil {
		t.Fatalf("ModifyFilesystem set quota: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), "sc-modq", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems after set: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Quota != 2097152 {
		t.Fatalf("expected quota %d, got %d", 2097152, listResult.Entries[0].Quota)
	}

	// Clear quota.
	if err := c.ModifyFilesystem(context.TODO(), "sc-modq", storage.Filesystem{Name: "sc-modq", Quota: 0}); err != nil {
		t.Fatalf("ModifyFilesystem clear quota: %v", err)
	}

	listResult, err = c.ListFilesystems(context.TODO(), "sc-modq", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems after clear: %v", err)
	}
	if listResult.Entries[0].Quota != 0 {
		t.Fatalf("expected quota 0 after clear, got %d", listResult.Entries[0].Quota)
	}
}

func TestSystemControllerNestedSubvolumes(t *testing.T) {
	c := initSystemControllerTest(t)

	names := []string{"sc-nest/parent", "sc-nest/parent/child", "sc-nest/parent/child/deep"}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	// Clean up deepest first, including the auto-created intermediate.
	allNames := append([]string{"sc-nest"}, names...)
	t.Cleanup(func() {
		for i := len(allNames) - 1; i >= 0; i-- {
			if err := c.RemoveFilesystem(context.TODO(), allNames[i]); err != nil {
				t.Errorf("cleanup RemoveFilesystem(%q): %v", allNames[i], err)
			}
		}
	})

	// All three should appear when listing with the parent prefix.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-nest/", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems with parent prefix: %v", err)
	}
	if len(listResult.Entries) != 3 {
		t.Fatalf("expected 3 filesystems under sc-nest/, got %d", len(listResult.Entries))
	}

	// Exact match should return only the leaf.
	listResult, err = c.ListFilesystems(context.TODO(), "sc-nest/parent/child/deep", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems exact: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem for exact match, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Name != "sc-nest/parent/child/deep" {
		t.Fatalf("expected %q, got %q", "sc-nest/parent/child/deep", listResult.Entries[0].Name)
	}
}

func TestSystemControllerNestedSubvolumeQuotaOnLeaf(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-leafq/parent"}); err != nil {
		t.Fatalf("CreateFilesystem parent: %v", err)
	}
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-leafq/parent/child", Quota: 4194304}); err != nil {
		t.Fatalf("CreateFilesystem child with quota: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-leafq/parent/child"); err != nil {
			t.Errorf("cleanup child: %v", err)
		}
		if err := c.RemoveFilesystem(context.TODO(), "sc-leafq/parent"); err != nil {
			t.Errorf("cleanup parent: %v", err)
		}
		if err := c.RemoveFilesystem(context.TODO(), "sc-leafq"); err != nil {
			t.Errorf("cleanup sc-leafq: %v", err)
		}
	})

	// Child should have its quota.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-leafq/parent/child", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems child: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Quota != 4194304 {
		t.Fatalf("expected child quota %d, got %d", 4194304, listResult.Entries[0].Quota)
	}

	// Parent should have no quota.
	listResult, err = c.ListFilesystems(context.TODO(), "sc-leafq/parent", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems parent: %v", err)
	}
	for _, fs := range listResult.Entries {
		if fs.Name == "sc-leafq/parent" && fs.Quota != 0 {
			t.Fatalf("expected parent quota 0, got %d", fs.Quota)
		}
	}
}

func TestSystemControllerQuotaUpdatePreservesName(t *testing.T) {
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-qname", Quota: 1024}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-qname"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	// Modify quota without changing name.
	if err := c.ModifyFilesystem(context.TODO(), "sc-qname", storage.Filesystem{Name: "sc-qname", Quota: 8192}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), "sc-qname", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}
	if listResult.Entries[0].Name != "sc-qname" {
		t.Fatalf("expected name %q, got %q", "sc-qname", listResult.Entries[0].Name)
	}
	if listResult.Entries[0].Quota != 8192 {
		t.Fatalf("expected quota %d, got %d", 8192, listResult.Entries[0].Quota)
	}
}

func TestSystemControllerCreateMultipleNestedWithQuotas(t *testing.T) {
	c := initSystemControllerTest(t)

	type fs struct {
		name  string
		quota uint64
	}
	filesystems := []fs{
		{"sc-mnq/data", 0},
		{"sc-mnq/data/logs", 1048576},
		{"sc-mnq/data/cache", 2097152},
		{"sc-mnq/data/media", 4194304},
	}

	for _, f := range filesystems {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: f.name, Quota: f.quota}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", f.name, err)
		}
	}
	// Clean up deepest first, including the auto-created intermediate.
	t.Cleanup(func() {
		for i := len(filesystems) - 1; i >= 0; i-- {
			if err := c.RemoveFilesystem(context.TODO(), filesystems[i].name); err != nil {
				t.Errorf("cleanup %q: %v", filesystems[i].name, err)
			}
		}
		if err := c.RemoveFilesystem(context.TODO(), "sc-mnq"); err != nil {
			t.Errorf("cleanup sc-mnq: %v", err)
		}
	})

	// List all children.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-mnq/data", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 4 {
		t.Fatalf("expected 4 filesystems, got %d", len(listResult.Entries))
	}

	// Verify individual quotas.
	quotaMap := map[string]uint64{}
	for _, f := range listResult.Entries {
		quotaMap[f.Name] = f.Quota
	}
	for _, want := range filesystems {
		got, ok := quotaMap[want.name]
		if !ok {
			t.Fatalf("missing filesystem %q in list", want.name)
		}
		if got != want.quota {
			t.Fatalf("filesystem %q: expected quota %d, got %d", want.name, want.quota, got)
		}
	}
}

// --- Purge volume integration tests ---

func TestSystemControllerPurgeVolumes(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	// Simulate package "test-repo/sc-purge" with two child volumes via direct storage.
	children := []string{"installed/test-repo/sc-purge/data", "installed/test-repo/sc-purge/logs"}
	for _, name := range children {
		if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Safety net: if purge fails, clean up manually.
	t.Cleanup(func() {
		for i := len(children) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(children[i])
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-purge")
	})

	// Verify children exist via direct storage.
	list, err := btr.ListFilesystems("installed/test-repo/sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems before purge: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 filesystems before purge, got %d", len(list))
	}

	// Purge all volumes for "test-repo/sc-purge".
	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-purge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	// Verify all children are gone.
	list, err = btr.ListFilesystems("installed/test-repo/sc-purge/")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems after purge, got %d", len(list))
	}

	// Verify the parent intermediate is also gone.
	list, err = btr.ListFilesystems("installed/test-repo/sc-purge")
	if err != nil {
		t.Fatalf("ListFilesystems parent after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected parent to be purged, got %d filesystems", len(list))
	}
}

func TestSystemControllerPurgeVolumesSimilarPrefix(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	// Create two packages with similar prefixes via direct storage.
	if err := btr.CreateFilesystem(storage.Filesystem{Name: "installed/test-repo/sc-pfx/data"}); err != nil {
		t.Fatalf("CreateFilesystem test-repo/sc-pfx/data: %v", err)
	}
	if err := btr.CreateFilesystem(storage.Filesystem{Name: "installed/test-repo/sc-pfx2/data"}); err != nil {
		t.Fatalf("CreateFilesystem test-repo/sc-pfx2/data: %v", err)
	}
	t.Cleanup(func() {
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx/data")
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx")
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx2/data")
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pfx2")
	})

	// Purge "sc-pfx" only.
	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-pfx"); err != nil {
		t.Fatalf("PurgeVolumes sc-pfx: %v", err)
	}

	// sc-pfx should be gone.
	list, err := btr.ListFilesystems("installed/test-repo/sc-pfx/")
	if err != nil {
		t.Fatalf("ListFilesystems installed/test-repo/sc-pfx/: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems for installed/test-repo/sc-pfx/, got %d", len(list))
	}

	// sc-pfx2 should survive.
	list, err = btr.ListFilesystems("installed/test-repo/sc-pfx2/")
	if err != nil {
		t.Fatalf("ListFilesystems installed/test-repo/sc-pfx2/: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 filesystem for installed/test-repo/sc-pfx2/, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesDeepNesting(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	names := []string{
		"installed/test-repo/sc-dpurge/a",
		"installed/test-repo/sc-dpurge/a/b",
		"installed/test-repo/sc-dpurge/a/b/c",
		"installed/test-repo/sc-dpurge/a/b/c/d",
	}
	for _, name := range names {
		if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(names[i])
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-dpurge")
	})

	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-dpurge"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := btr.ListFilesystems("installed/test-repo/sc-dpurge")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 filesystems after purge, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesNonexistent(t *testing.T) {
	c := initSystemControllerTest(t)

	// Create a filesystem to verify it's not affected.
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-surv"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	t.Cleanup(func() {
		_ = c.RemoveFilesystem(context.TODO(), "sc-surv")
	})

	// Purge a package that doesn't exist — should succeed.
	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-nonexistent"); err != nil {
		t.Fatalf("PurgeVolumes nonexistent: %v", err)
	}

	// The unrelated filesystem should still exist.
	listResult, err := c.ListFilesystems(context.TODO(), "sc-surv", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem to survive, got %d", len(listResult.Entries))
	}
}

func TestSystemControllerPurgeVolumesWithQuotas(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	children := []storage.Filesystem{
		{Name: "installed/test-repo/sc-pq/data", Quota: 1048576},
		{Name: "installed/test-repo/sc-pq/logs", Quota: 2097152},
	}
	for _, f := range children {
		if err := btr.CreateFilesystem(f); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", f.Name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(children) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(children[i].Name)
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pq")
	})

	// Verify quotas exist before purge via direct storage.
	list, err := btr.ListFilesystems("installed/test-repo/sc-pq/")
	if err != nil {
		t.Fatalf("ListFilesystems before purge: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	for _, fs := range list {
		if fs.Quota == 0 {
			t.Fatalf("expected non-zero quota for %q before purge", fs.Name)
		}
	}

	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-pq"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err = btr.ListFilesystems("installed/test-repo/sc-pq")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 after purge, got %d", len(list))
	}
}

func TestSystemControllerPurgeVolumesMultipleChildren(t *testing.T) {
	c, btr := initSystemControllerTestWithStorage(t)

	names := []string{
		"installed/test-repo/sc-pmulti/alpha",
		"installed/test-repo/sc-pmulti/bravo",
		"installed/test-repo/sc-pmulti/charlie",
		"installed/test-repo/sc-pmulti/delta",
		"installed/test-repo/sc-pmulti/echo",
	}
	for _, name := range names {
		if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			_ = btr.RemoveFilesystem(names[i])
		}
		_ = btr.RemoveFilesystem("installed/test-repo/sc-pmulti")
	})

	if err := c.PurgeVolumes(context.TODO(), "test-repo", "sc-pmulti"); err != nil {
		t.Fatalf("PurgeVolumes: %v", err)
	}

	list, err := btr.ListFilesystems("installed/test-repo/sc-pmulti")
	if err != nil {
		t.Fatalf("ListFilesystems after purge: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 after purge, got %d", len(list))
	}
}
