// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// An upgrade renames installed/<repo>/<pkg>/<old>/<vol> to the same path under
// <new>. RenameFilesystem is os.Rename, which cannot create the destination's
// parent, and nothing else had created it: provisionVolumes builds
// installed/<repo>/<pkg>/<new>/ only after prepareActiveVersion returns. So the
// rename failed ENOENT on every upgrade, the failure was logged at Debug, and
// the package came up on a brand new empty volume -- for plex, losing the claim
// token in Preferences.xml (the server answers "not authorized" again) along
// with the library database.
func TestEnsureVolumeParentMakesTheUpgradeRenamePossible(t *testing.T) {
	base := t.TempDir()

	src := packageVolumePath("repo-a", "plex", "3.0", "config")
	dst := packageVolumePath("repo-a", "plex", "3.1", "config")

	srcPath := filepath.Join(base, src)
	if err := os.MkdirAll(srcPath, 0o750); err != nil {
		t.Fatalf("create source volume: %v", err)
	}
	// Stands in for Preferences.xml: the state the upgrade has to carry over.
	if err := os.WriteFile(filepath.Join(srcPath, "claimed"), []byte("PlexOnlineToken=x"), 0o600); err != nil {
		t.Fatalf("seed source volume: %v", err)
	}

	dstPath := filepath.Join(base, dst)

	// The precondition is the bug: on its own the rename cannot succeed.
	err := os.Rename(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected the rename to fail with no destination parent present")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected a not-exist error, got %v", err)
	}

	if err := ensureVolumeParent(base, dst); err != nil {
		t.Fatalf("ensureVolumeParent: %v", err)
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		t.Fatalf("rename after ensureVolumeParent: %v", err)
	}

	moved, err := os.ReadFile(filepath.Join(dstPath, "claimed"))
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if string(moved) != "PlexOnlineToken=x" {
		t.Fatalf("migrated file content = %q, want the seeded value", string(moved))
	}
}

// An empty base is the mocked-backend case. It must be a no-op rather than an
// error, and above all must not create "installed/..." relative to the process's
// working directory, which for a unit test is the package source directory.
func TestEnsureVolumeParentIgnoresAnEmptyBase(t *testing.T) {
	dst := packageVolumePath("repo-a", "plex", "3.1", "config")

	if err := ensureVolumeParent("", dst); err != nil {
		t.Fatalf("ensureVolumeParent with an empty base: %v", err)
	}

	if _, err := os.Stat(PackagesVolumePrefix); err == nil {
		t.Fatalf("ensureVolumeParent created %q relative to the working directory", PackagesVolumePrefix)
	}
}

// The reuse path (reinstalling a package that was uninstalled) renames
// uninstalled/<repo>/<pkg> to installed/<repo>/<pkg> and has the same shape of
// bug: on a box where no other package from that repo is installed,
// installed/<repo>/ does not exist yet.
func TestEnsureVolumeParentMakesTheReuseRenamePossible(t *testing.T) {
	base := t.TempDir()

	src := UninstalledVolumePrefix + "/repo-a/plex"
	dst := PackagesVolumePrefix + "/repo-a/plex"

	srcPath := filepath.Join(base, src)
	if err := os.MkdirAll(srcPath, 0o750); err != nil {
		t.Fatalf("create uninstalled tree: %v", err)
	}

	dstPath := filepath.Join(base, dst)
	if err := os.Rename(srcPath, dstPath); err == nil {
		t.Fatal("expected the reuse rename to fail with no destination parent present")
	}

	if err := ensureVolumeParent(base, dst); err != nil {
		t.Fatalf("ensureVolumeParent: %v", err)
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		t.Fatalf("reuse rename after ensureVolumeParent: %v", err)
	}
}
