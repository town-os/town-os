// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupStaleRootDBRemovesAllSidecars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	files := []string{
		"town-os.db",
		"town-os.db-wal",
		"town-os.db-shm",
		"town-os.db-journal",
	}
	for _, name := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}

	// Drop an unrelated file at the same root to make sure we don't
	// over-delete.
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("seed keep file: %v", err)
	}

	cleanupStaleRootDB(dir)

	for _, name := range files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %s removed, got err=%v", path, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated file should not have been touched: %v", err)
	}
}

func TestCleanupStaleRootDBIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Calling against an empty directory must not error or create files.
	cleanupStaleRootDB(dir)
	cleanupStaleRootDB(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir after cleanup, got %d entries", len(entries))
	}
}

func TestCleanupStaleRootDBEmptyBase(t *testing.T) {
	t.Parallel()
	// Empty base must be a no-op rather than touching the cwd.
	cleanupStaleRootDB("")
}

func TestValidateDBPathRejectsStaleRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stale := filepath.Join(dir, "town-os.db")

	err := validateDBPath(stale, dir)
	if err == nil {
		t.Fatal("expected validateDBPath to reject stale root path")
	}
	if !strings.Contains(err.Error(), "town-os.db") {
		t.Errorf("error should mention town-os.db, got %q", err)
	}
}

func TestValidateDBPathRejectsRelativeStaleRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A trailing slash + redundant separators should still match after
	// filepath.Abs/Clean normalisation.
	stale := dir + "/./town-os.db"

	if err := validateDBPath(stale, dir); err == nil {
		t.Fatal("expected validateDBPath to reject stale root after normalisation")
	}
}

func TestValidateDBPathAllowsSubdirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "data", "db", "system.db")

	if err := validateDBPath(good, dir); err != nil {
		t.Errorf("expected subdirectory path to be accepted, got %v", err)
	}
}

func TestValidateDBPathEmptyArgsAccepted(t *testing.T) {
	t.Parallel()
	if err := validateDBPath("", "/town-os"); err != nil {
		t.Errorf("empty -db must be accepted, got %v", err)
	}
	if err := validateDBPath("/tmp/x.db", ""); err != nil {
		t.Errorf("empty -btrfs must be accepted, got %v", err)
	}
}
