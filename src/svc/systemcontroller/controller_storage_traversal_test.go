// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

// The user/ prefix the storage handlers prepend is the whole of a user volume's
// namespacing, and a prefix is not a boundary unless what follows it cannot
// climb back out. filepath.Join collapses "..", so `../gfeh/home` became
// `user/../gfeh/home` and addressed another network's object-storage partition
// — and it slipped past isReservedFilesystem too, which matches on a leading
// prefix the traversal does not carry yet.
//
// These routes are requireAuth, not requireAdmin, so this was reachable by any
// ordinary account on the box.

// subvolExists lives in controller_storage_cascade_test.go.

func TestRemoveFilesystemRejectsTraversal(t *testing.T) {
	victims := []string{
		"gfeh/home",
		"installed/core/gitea/1.0/data",
		"pages/blog.home",
	}

	for _, victim := range victims {
		t.Run(victim, func(t *testing.T) {
			c, ctrl := initTestClient(t)
			injectSubvol(t, ctrl, victim, 0)

			// The name the caller submits is relative to user/, so climbing one
			// level lands on the btrfs root.
			if err := c.RemoveFilesystem(context.TODO(), "../"+victim); err == nil {
				t.Fatal("RemoveFilesystem accepted a traversing name")
			}

			if !subvolExists(ctrl, victim) {
				t.Fatalf("%q was deleted through the user/ prefix", victim)
			}
		})
	}
}

func TestRemoveFilesystemRejectsAbsolutePath(t *testing.T) {
	c, ctrl := initTestClient(t)
	injectSubvol(t, ctrl, "gfeh/home", 0)

	if err := c.RemoveFilesystem(context.TODO(), "/gfeh/home"); err == nil {
		t.Fatal("RemoveFilesystem accepted an absolute name")
	}
	if !subvolExists(ctrl, "gfeh/home") {
		t.Fatal("gfeh/home was deleted through an absolute name")
	}
}

// The rename half: ModifyFilesystem validated only the name being renamed TO,
// so a traversing source moved somebody else's subvolume into the caller's own
// namespace — the data comes along with it.
func TestModifyFilesystemRejectsTraversalInSourceName(t *testing.T) {
	c, ctrl := initTestClient(t)
	injectSubvol(t, ctrl, "gfeh/home", 0)

	err := c.ModifyFilesystem(context.TODO(), "../gfeh/home", storage.Filesystem{Name: "stolen"})
	if err == nil {
		t.Fatal("ModifyFilesystem accepted a traversing source name")
	}

	if !subvolExists(ctrl, "gfeh/home") {
		t.Fatal("gfeh/home was renamed out from under object storage")
	}
	if subvolExists(ctrl, "user/stolen") {
		t.Fatal("a partition was renamed into the user namespace")
	}
}

func TestModifyFilesystemRejectsTraversalInTargetName(t *testing.T) {
	c, ctrl := initTestClient(t)
	injectSubvol(t, ctrl, "user/mine", 0)

	err := c.ModifyFilesystem(context.TODO(), "mine", storage.Filesystem{Name: "../gfeh/planted"})
	if err == nil {
		t.Fatal("ModifyFilesystem accepted a traversing target name")
	}
	if subvolExists(ctrl, "gfeh/planted") {
		t.Fatal("a user volume was renamed into the object-storage root")
	}
}

// /storage/remove-package-volume checks that the name starts with installed/
// or uninstalled/ and passes it straight through, so `installed/../gfeh/home`
// satisfied the prefix check and addressed something else entirely. It is
// admin-only, so this is defence in depth rather than an escalation — the
// storage layer is what refuses it now.
func TestRemovePackageVolumeRejectsTraversal(t *testing.T) {
	c, ctrl := initTestClient(t)
	injectSubvol(t, ctrl, "gfeh/home", 0)

	if err := c.RemovePackageVolume(context.TODO(), "installed/../gfeh/home"); err == nil {
		t.Fatal("RemovePackageVolume accepted a traversing internal name")
	}
	if !subvolExists(ctrl, "gfeh/home") {
		t.Fatal("gfeh/home was deleted through the installed/ prefix")
	}
}

func TestCreateFilesystemRejectsTraversal(t *testing.T) {
	c, ctrl := initTestClient(t)

	err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "../gfeh/planted"})
	if err == nil {
		t.Fatal("CreateFilesystem accepted a traversing name")
	}
	if subvolExists(ctrl, "gfeh/planted") {
		t.Fatal("a volume was created under the object-storage root")
	}
}

// A list prefix is not a name, and must not be validated as one.
//
// `sc-nest/` means "everything under sc-nest" and a name validator rejects it
// for ending in an empty path component — which is how the write-path
// validation, applied here by mistake, broke nested-volume listing. Nothing on
// this path becomes a filesystem path: the storage layer lists from its own
// base and uses this only as a string filter.
func TestListFilesystemsAcceptsTrailingSlashPrefix(t *testing.T) {
	c, ctrl := initTestClient(t)
	for _, name := range []string{"user/nest/parent", "user/nest/parent/child"} {
		injectSubvol(t, ctrl, name, 0)
	}

	result, err := c.ListFilesystems(context.TODO(), "nest/", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems with a trailing-slash prefix: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("got %d entries under nest/, want 2", len(result.Entries))
	}
}

// And a traversing prefix is harmless rather than an error: user/ is prepended
// unconditionally, so it matches nothing instead of reaching anything.
func TestListFilesystemsTraversalPrefixMatchesNothing(t *testing.T) {
	c, ctrl := initTestClient(t)
	injectSubvol(t, ctrl, "gfeh/home", 0)
	injectSubvol(t, ctrl, "user/mine", 0)

	result, err := c.ListFilesystems(context.TODO(), "../gfeh/", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems with a traversing prefix: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("traversing prefix matched %d entries, want none: %+v", len(result.Entries), result.Entries)
	}
}

// The ordinary path must keep working — nested user volumes are legitimate and
// the validator has to admit the characters real names use.
func TestCreateAndRemoveFilesystemAcceptsOrdinaryNames(t *testing.T) {
	names := []string{"photos", "nested/path", "with-dash", "with_underscore", "with.dot"}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			c, ctrl := initTestClient(t)

			if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
				t.Fatalf("CreateFilesystem(%q): %v", name, err)
			}
			if !subvolExists(ctrl, "user/"+name) {
				t.Fatalf("user/%s was not created", name)
			}
			if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
				t.Fatalf("RemoveFilesystem(%q): %v", name, err)
			}
			if subvolExists(ctrl, "user/"+name) {
				t.Fatalf("user/%s survived removal", name)
			}
		})
	}
}
