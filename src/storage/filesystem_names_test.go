// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"errors"
	"testing"
)

// seedMockFilesystems replaces the mock controller's subvolume list with
// the given names, bypassing BtrFS.CreateFilesystem's intermediate-
// subvolume auto-creation so tests can verify exact list contents.
func seedMockFilesystems(t *testing.T, mock *BtrFS, names ...string) {
	t.Helper()
	ctrl, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatalf("expected MockBtrFSController, got %T", mock.Controller)
	}
	ctrl.Lock.Lock()
	defer ctrl.Lock.Unlock()
	ctrl.Filesystems = nil
	for _, n := range names {
		ctrl.addFilesystemLocked(n)
	}
}

func TestFilesystemNamesReturnsAllNames(t *testing.T) {
	mock := InitBtrFSMock()
	input := []string{"user/alice", "installed/default/nginx/1.0/data", "uninstalled/default/redis/1.0/data"}
	seedMockFilesystems(t, mock, input...)

	names, err := mock.FilesystemNames("")
	if err != nil {
		t.Fatalf("FilesystemNames: %v", err)
	}

	if len(names) != len(input) {
		t.Fatalf("expected %d names, got %d: %v", len(input), len(names), names)
	}

	want := map[string]bool{
		"user/alice":                         true,
		"installed/default/nginx/1.0/data":   true,
		"uninstalled/default/redis/1.0/data": true,
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected name %q", n)
		}
	}
}

func TestFilesystemNamesPrefixFilter(t *testing.T) {
	mock := InitBtrFSMock()
	seedMockFilesystems(t, mock, "user/alice", "installed/a/b/1.0/data", "installed/a/b/1.0/logs", "uninstalled/c/d/1.0/data")

	names, err := mock.FilesystemNames("installed/")
	if err != nil {
		t.Fatalf("FilesystemNames: %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 installed/ names, got %d: %v", len(names), names)
	}
	for _, n := range names {
		if n != "installed/a/b/1.0/data" && n != "installed/a/b/1.0/logs" {
			t.Errorf("unexpected name %q in installed/ filter", n)
		}
	}
}

// TestFilesystemNamesDoesNotCallQGroupShow pins the core performance
// invariant: FilesystemNames must list subvolumes without invoking
// `btrfs qgroup show` per subvolume. The ping handler calls this in its
// hot path and the whole point is to avoid the O(N) CLI fork storm that
// ListFilesystems incurs.
func TestFilesystemNamesDoesNotCallQGroupShow(t *testing.T) {
	mock := InitBtrFSMock()
	ctrl, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatalf("expected MockBtrFSController, got %T", mock.Controller)
	}

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if err := mock.CreateFilesystem(Filesystem{Name: name, Quota: 4096}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Discard the call log accumulated during setup so we only inspect
	// what FilesystemNames itself invokes.
	ctrl.Lock.Lock()
	ctrl.Call = nil
	ctrl.Lock.Unlock()

	names, err := mock.FilesystemNames("")
	if err != nil {
		t.Fatalf("FilesystemNames: %v", err)
	}
	if len(names) != 5 {
		t.Fatalf("expected 5 names, got %d", len(names))
	}

	calls := ctrl.GetLog()
	subvolListCount := 0
	for _, c := range calls {
		if c.Operation == "QGroupShow" || c.Operation == "SubvolID" {
			t.Errorf("FilesystemNames invoked %s — should skip quota lookups entirely; args=%v", c.Operation, c.Arguments)
		}
		if c.Operation == "SubvolList" {
			subvolListCount++
		}
	}
	if subvolListCount != 1 {
		t.Errorf("expected exactly 1 SubvolList call, got %d", subvolListCount)
	}
}

func TestFilesystemNamesPropagatesError(t *testing.T) {
	mock := InitBtrFSMock()
	ctrl, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatalf("expected MockBtrFSController, got %T", mock.Controller)
	}
	ctrl.SubvolListErr = errors.New("btrfs down")

	if _, err := mock.FilesystemNames(""); err == nil {
		t.Fatal("expected error when SubvolList fails, got nil")
	}
}

func TestFilesystemNamesReturnsEmptyOnMountNotFound(t *testing.T) {
	mock := InitBtrFSMock()
	ctrl, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatalf("expected MockBtrFSController, got %T", mock.Controller)
	}
	ctrl.SubvolListErr = ErrMountNotFound

	names, err := mock.FilesystemNames("")
	if err != nil {
		t.Fatalf("ErrMountNotFound should be swallowed, got: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty slice, got %v", names)
	}
}
