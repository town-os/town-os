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

	// Use a unique prefix so parallel tests don't affect our count.
	const prefix = "sc-create-list"

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: prefix}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}
	t.Cleanup(func() {
		if err := c.RemoveFilesystem(context.TODO(), prefix); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", prefix, err)
		}
	})

	listResult, err := c.ListFilesystems(context.TODO(), prefix, "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing with prefix: %v", err)
	}

	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem with prefix %q, got %d", prefix, len(listResult.Entries))
	}
}

func TestSystemControllerRemove(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	const prefix = "sc-remove"

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: prefix}); err != nil {
		t.Fatalf("error creating filesystem: %v", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), prefix); err != nil {
		t.Fatalf("error removing filesystem: %v", err)
	}

	listResult, err := c.ListFilesystems(context.TODO(), prefix, "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(listResult.Entries) != 0 {
		t.Fatalf("expected 0 filesystems with prefix %q after remove, got %d", prefix, len(listResult.Entries))
	}
}

func TestSystemControllerMultipleFilesystems(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	const prefix = "sc-multi"
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

	listResult, err := c.ListFilesystems(context.TODO(), prefix, "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after creates: %v", err)
	}

	if len(listResult.Entries) != len(names) {
		t.Fatalf("expected %d filesystems with prefix %q, got %d", len(names), prefix, len(listResult.Entries))
	}

	// Remove one and verify count
	if err := c.RemoveFilesystem(context.TODO(), "sc-multi-b"); err != nil {
		t.Fatalf("error removing sc-multi-b: %v", err)
	}

	listResult, err = c.ListFilesystems(context.TODO(), prefix, "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after partial remove: %v", err)
	}

	if len(listResult.Entries) != len(names)-1 {
		t.Fatalf("expected %d filesystems with prefix %q after partial remove, got %d", len(names)-1, prefix, len(listResult.Entries))
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

	// Quota must exceed the ~16KiB an empty subvolume already references;
	// newer kernels reject qgroup limits below current usage with EDQUOT.
	if err := c.ModifyFilesystem(context.TODO(), "sc-modify", storage.Filesystem{Name: "sc-modify", Quota: 1048576}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}
}

func TestSystemControllerModifyPackageVolumeRenameRejected(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorage(t)

	volPath := "installed/rename-rej-repo/rename-rej-pkg/1.0/data"
	if err := btr.CreateFilesystem(storage.Filesystem{Name: volPath}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", volPath, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(volPath); err != nil {
			t.Logf("cleanup RemoveFilesystem(%q): %v", volPath, err)
		}
	})

	err := c.ModifyFilesystem(context.TODO(), volPath, storage.Filesystem{
		Name: "installed/rename-rej-repo/rename-rej-pkg/1.0/renamed",
	})
	if err == nil {
		t.Fatal("expected error when renaming installed package volume")
	}
}

func TestSystemControllerModifyPackageVolumeQuotaAllowed(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorage(t)

	volPath := "installed/quota-ok-repo/quota-ok-pkg/1.0/data"
	if err := btr.CreateFilesystem(storage.Filesystem{Name: volPath}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", volPath, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(volPath); err != nil {
			t.Logf("cleanup RemoveFilesystem(%q): %v", volPath, err)
		}
	})

	// Quota must exceed the ~16KiB an empty subvolume already references;
	// newer kernels reject qgroup limits below current usage with EDQUOT.
	if err := c.ModifyFilesystem(context.TODO(), volPath, storage.Filesystem{
		Name:  volPath,
		Quota: 4194304,
	}); err != nil {
		t.Fatalf("ModifyFilesystem quota on installed volume: %v", err)
	}
}

func TestSystemControllerFullLifecycle(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	const prefix = "sc-lifecycle"

	// Create
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: prefix}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify exists
	listResult, err := c.ListFilesystems(context.TODO(), prefix, "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems to verify creation: %v", err)
	}
	if len(listResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem with prefix %q, got %d", prefix, len(listResult.Entries))
	}

	// Remove
	if err := c.RemoveFilesystem(context.TODO(), prefix); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Verify gone
	listResult, err = c.ListFilesystems(context.TODO(), prefix, "", systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems to verify removal: %v", err)
	}
	if len(listResult.Entries) != 0 {
		t.Fatalf("expected 0 filesystems with prefix %q after remove, got %d", prefix, len(listResult.Entries))
	}
}
