// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

func TestSystemControllerCreateAndList(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-create-list"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-create-list"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-create-list", err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after create: %v", err)
	}

	if len(listResult.Entries) != baseCount+1 {
		t.Fatalf("expected %d filesystems after create, got %d", baseCount+1, len(listResult.Entries))
	}

	listResult, err = c.ListFilesystems(context.TODO(), "sc-create-list", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing with exact prefix: %v", err)
	}

	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem under test path, got %d", len(listResult.Entries))
	}
}

func TestSystemControllerRemove(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-remove"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "sc-remove"); err != nil {
		t.Fatalf("error removing filesystem: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(listResult.Entries) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(listResult.Entries))
	}
}

func TestSystemControllerMultipleFilesystems(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	names := []string{"sc-multi-a", "sc-multi-b", "sc-multi-c"}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("error creating %q: %v", name, err)
		}
		// sc-multi-b is explicitly removed in the test body below.
		if name != "sc-multi-b" {
			name := name
			t.Cleanup(func() {
				if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
					t.Errorf("cleanup RemoveFilesystem(%q): %v", name, err)
				}
			})
		}
	}

	listResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after creates: %v", err)
	}

	if len(listResult.Entries) != baseCount+len(names) {
		t.Fatalf("expected %d filesystems, got %d", baseCount+len(names), len(listResult.Entries))
	}

	// Remove one and verify count
	if err := c.RemoveFilesystem(context.TODO(), "sc-multi-b"); err != nil {
		t.Fatalf("error removing sc-multi-b: %v", err)
	}

	listResult, err = c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after partial remove: %v", err)
	}

	if len(listResult.Entries) != baseCount+len(names)-1 {
		t.Fatalf("expected %d filesystems after partial remove, got %d", baseCount+len(names)-1, len(listResult.Entries))
	}
}

func TestSystemControllerListPrefix(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-pfx-a"}); err != nil {
		t.Fatalf("error creating sc-pfx-a: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-pfx-a"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-pfx-a", err)
		}
	})

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-pfx-b"}); err != nil {
		t.Fatalf("error creating sc-pfx-b: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-pfx-b"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-pfx-b", err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), "sc-pfx-a", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing with prefix: %v", err)
	}

	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem for prefix sc-pfx-a, got %d", len(listResult.Entries))
	}
}

func TestSystemControllerModifyFilesystem(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-modify"}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), "sc-modify"); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", "sc-modify", err)
		}
	})

	if err := c.ModifyFilesystem(context.TODO(), "sc-modify", storage.Filesystem{Name: "sc-modify", Quota: 1024}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}
}

func TestSystemControllerModifyPackageVolumeRenameRejected(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorage(t)

	volPath := "installed/repo/pkg/1.0/data"
	if err := btr.CreateFilesystem(storage.Filesystem{Name: volPath}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", volPath, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(volPath); err != nil {
			t.Logf("cleanup RemoveFilesystem(%q): %v", volPath, err)
		}
	})

	err := c.ModifyFilesystem(context.TODO(), volPath, storage.Filesystem{
		Name: "installed/repo/pkg/1.0/renamed",
	})
	if err == nil {
		t.Fatal("expected error when renaming installed package volume")
	}
}

func TestSystemControllerModifyPackageVolumeQuotaAllowed(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorage(t)

	volPath := "installed/repo/pkg/1.0/data"
	if err := btr.CreateFilesystem(storage.Filesystem{Name: volPath}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", volPath, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(volPath); err != nil {
			t.Logf("cleanup RemoveFilesystem(%q): %v", volPath, err)
		}
	})

	if err := c.ModifyFilesystem(context.TODO(), volPath, storage.Filesystem{
		Name:  volPath,
		Quota: 4096,
	}); err != nil {
		t.Fatalf("ModifyFilesystem quota on installed volume: %v", err)
	}
}

func TestSystemControllerFullLifecycle(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	baseResult, err := c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems before create: %v", err)
	}
	baseCount := len(baseResult.Entries)

	// Create
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "sc-lifecycle"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify exists
	listResult, err := c.ListFilesystems(context.TODO(), "sc-lifecycle", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems to verify creation: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(listResult.Entries))
	}

	// Remove
	if err := c.RemoveFilesystem(context.TODO(), "sc-lifecycle"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Verify gone
	listResult, err = c.ListFilesystems(context.TODO(), "", "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems to verify removal: %v", err)
	}
	if len(listResult.Entries) != baseCount {
		t.Fatalf("expected %d filesystems after remove, got %d", baseCount, len(listResult.Entries))
	}
}
