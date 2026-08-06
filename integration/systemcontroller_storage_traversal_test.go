// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

// The mocked unit tests prove the handlers refuse a traversing name. These
// prove it against real btrfs: the subvolume the traversal aimed at is created
// for real, the request is made through the real HTTP stack, and the subvolume
// is still there afterwards.
//
// Every name is derived from the test name so two of these — or two concurrent
// `make test-full` runs sharing a filesystem — never address the same
// subvolume.

// traversalVictim creates a subvolume outside the user/ namespace and registers
// its removal. It goes through the storage handle directly because the point of
// the fix is that the HTTP API cannot address this prefix at all.
func traversalVictim(t *testing.T, btr *storage.BtrFS, prefix string) string {
	t.Helper()

	name := fmt.Sprintf("%s/%s", prefix, strings.ToLower(t.Name()))
	if err := btr.CreateFilesystem(storage.Filesystem{Name: name}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", name, err)
	}
	t.Cleanup(func() {
		if err := btr.RemoveFilesystem(name); err != nil {
			t.Errorf("cleanup RemoveFilesystem(%q): %v", name, err)
		}
	})
	return name
}

// subvolumePresent reports whether a subvolume is listed under its own name.
func subvolumePresent(t *testing.T, btr *storage.BtrFS, name string) bool {
	t.Helper()

	names, err := btr.FilesystemNames("")
	if err != nil {
		t.Fatalf("FilesystemNames: %v", err)
	}
	return slices.Contains(names, name)
}

func TestSystemControllerStorageRemoveTraversalRejected(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)

	victim := traversalVictim(t, btr, "gfeh")

	// The submitted name is relative to user/, so one level up is the btrfs
	// root and the rest addresses the object-storage partition.
	if err := c.RemoveFilesystem(context.TODO(), "../"+victim); err == nil {
		t.Fatal("RemoveFilesystem accepted a traversing name")
	}

	if !subvolumePresent(t, btr, victim) {
		t.Fatalf("%q was deleted through the user/ prefix", victim)
	}
}

func TestSystemControllerStorageModifyTraversalRejected(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)

	victim := traversalVictim(t, btr, "gfeh")
	stolen := strings.ToLower(t.Name()) + "-stolen"

	err := c.ModifyFilesystem(context.TODO(), "../"+victim, storage.Filesystem{Name: stolen})
	if err == nil {
		t.Fatal("ModifyFilesystem accepted a traversing source name")
	}

	if !subvolumePresent(t, btr, victim) {
		t.Fatalf("%q was renamed out from under object storage", victim)
	}
	if subvolumePresent(t, btr, "user/"+stolen) {
		t.Fatalf("%q was renamed into the user namespace", victim)
	}
}

func TestSystemControllerStorageCreateTraversalRejected(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)

	planted := "gfeh/" + strings.ToLower(t.Name())

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "../" + planted}); err == nil {
		// Only reached on a regression; clean up so the next run is not
		// tripped by a stray subvolume.
		t.Cleanup(func() {
			if rerr := btr.RemoveFilesystem(planted); rerr != nil {
				t.Errorf("cleanup RemoveFilesystem(%q): %v", planted, rerr)
			}
		})
		t.Fatal("CreateFilesystem accepted a traversing name")
	}

	if subvolumePresent(t, btr, planted) {
		t.Fatalf("%q was created under the object-storage root", planted)
	}
}

// The ordinary path still works on real btrfs — the validator has to admit the
// names user volumes actually use.
func TestSystemControllerStorageOrdinaryNamesStillWork(t *testing.T) {
	t.Parallel()
	c, btr := initSystemControllerTestWithStorageAndBtrfsBase(t)

	name := strings.ToLower(t.Name())
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
		t.Fatalf("CreateFilesystem(%q): %v", name, err)
	}
	if !subvolumePresent(t, btr, "user/"+name) {
		t.Fatalf("user/%s was not created", name)
	}
	if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
		t.Fatalf("RemoveFilesystem(%q): %v", name, err)
	}
	if subvolumePresent(t, btr, "user/"+name) {
		t.Fatalf("user/%s survived removal", name)
	}
}
