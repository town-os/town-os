// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"math/rand/v2"
	"strconv"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestBtrFS(t *testing.T) {
	t.Parallel()
	btr := storage.InitBtrFS("/town-os")

	// Unique subvolume name so parallel runs can't clash on it.
	name := "btrfs-test-" + strconv.FormatUint(rand.Uint64(), 36)

	baseList, err := btr.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error while listing filesystems before create: %v", err)
	}
	baseCount := len(baseList)

	if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
		t.Fatalf("Could not create filesystem %s: %v", name, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(name); err != nil {
			t.Logf("cleanup RemoveFilesystem(%s): %v", name, err)
		}
	})

	list, err := btr.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	// Another parallel test may have created/removed subvolumes in the
	// meantime, so only assert that our own subvolume is visible.
	found := false
	for _, fs := range list {
		if fs.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %s in filesystem list after create (baseCount=%d, total=%d)", name, baseCount, len(list))
	}

	list, err = btr.ListFilesystems(name)
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Expected 1 filesystem under %s path, got %d", name, len(list))
	}

	if err := btr.RemoveFilesystem(name); err != nil {
		t.Fatalf("Could not remove filesystem %s: %v", name, err)
	}

	list, err = btr.ListFilesystems(name)
	if err != nil {
		t.Fatalf("Error while listing filesystems after remove: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("Expected 0 filesystems under %s after remove, got %d", name, len(list))
	}
}
