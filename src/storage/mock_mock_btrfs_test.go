// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"errors"
	"testing"
)

func TestMockBtrFSStorage(t *testing.T) {
	mock := InitBtrFSMock()
	err := mock.CreateFilesystem(Filesystem{Name: "test"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("Len should be 1 and was not: %d", len(fs))
	}

	err = mock.RemoveFilesystem("test")
	if err != nil {
		t.Fatalf("RemoveFilesystem: %v", err)
	}

	fs, err = mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems after remove: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("Len should be 0 and was not: %d", len(fs))
	}
}

func TestMockBtrFSBasic(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("test")
	if err != nil {
		t.Fatalf("SubvolCreate test: %v", err)
	}

	err = mock.SubvolCreate("test/sub")
	if err != nil {
		t.Fatalf("SubvolCreate test/sub: %v", err)
	}

	info := mock.GetFilesystems()

	if len(info) != 2 {
		t.Fatal("Not all filesystems were recorded")
	}

	table := []struct {
		Name string
		ID   uint64
	}{
		{
			Name: "test",
			ID:   1,
		},
		{
			Name: "test/sub",
			ID:   2,
		},
	}

	for x, item := range table {
		if info[x].Name != item.Name {
			t.Fatalf("Filesystems were not created in order (name check %d): %v", x, info[x].Name)
		}

		if info[x].ID != item.ID {
			t.Fatalf("Filesystems were not created in order (id check %d): %v", x, info[x].ID)
		}

		id, err := mock.SubvolID(item.Name)
		if err != nil {
			t.Fatalf("Received error getting ID for %q subvolume (%d): %v", item.Name, x, err)
		}

		if id != item.ID {
			t.Fatalf("Invalid ID for %q subvolume (%d): expected: %d, actual: %d", item.Name, x, item.ID, id)
		}
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 2 {
		t.Fatal("Invalid number of results listing test subvolumes")
	}

	if info[0].Name != "test" {
		t.Fatal("test volume does not exist in list call")
	}

	if info[0].ID != 1 {
		t.Fatal("test volume does not exist in list call (id check)")
	}

	if info[1].Name != "test/sub" {
		t.Fatal("test/sub volume does not exist in list call")
	}

	if info[1].ID != 2 {
		t.Fatal("test/sub volume does not exist in list call (id check)")
	}

	err = mock.SubvolDelete("test/sub")
	if err != nil {
		t.Fatalf("SubvolDelete test/sub: %v", err)
	}

	info, err = mock.SubvolList("test/sub")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test/sub` was not deleted after delete call")
	}

	err = mock.SubvolDelete("test")
	if err != nil {
		t.Fatalf("SubvolDelete test: %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}

	err = mock.SubvolCreate("test")
	if err != nil {
		t.Fatalf("SubvolCreate test (re-create): %v", err)
	}

	err = mock.SubvolCreate("test/sub")
	if err != nil {
		t.Fatalf("SubvolCreate test/sub (re-create): %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 2 {
		t.Fatal("Volumes were not re-created")
	}

	// Deleting a parent with children must fail (matches real btrfs behavior).
	err = mock.SubvolDelete("test")
	if err == nil {
		t.Fatal("SubvolDelete test should fail when child subvolumes exist")
	}

	// Delete child first, then parent.
	err = mock.SubvolDelete("test/sub")
	if err != nil {
		t.Fatalf("SubvolDelete test/sub (final child): %v", err)
	}

	err = mock.SubvolDelete("test")
	if err != nil {
		t.Fatalf("SubvolDelete test (final): %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}
}

func TestMockIsSubvolume(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.IsSubvolume("nonexistent")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}

	err = mock.SubvolCreate("test")
	if err != nil {
		t.Fatalf("SubvolCreate test: %v", err)
	}

	err = mock.IsSubvolume("test")
	if err != nil {
		t.Fatalf("expected nil error for existing subvolume, got %v", err)
	}

	err = mock.IsSubvolume("test/sub")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem for non-existing sub, got %v", err)
	}
}

func TestMockSubvolSnapshot(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("src")
	if err != nil {
		t.Fatalf("SubvolCreate src: %v", err)
	}

	err = mock.SubvolSnapshot("dst", "src", true)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	log := mock.GetLog()
	found := false
	for _, c := range log {
		if c.Operation == "SubvolSnapshot" {
			found = true
			if len(c.Arguments) != 3 {
				t.Fatalf("expected 3 arguments, got %d", len(c.Arguments))
			}
			if c.Arguments[0] != "dst" || c.Arguments[1] != "src" || c.Arguments[2] != true {
				t.Fatalf("unexpected snapshot arguments: %v", c.Arguments)
			}
		}
	}
	if !found {
		t.Fatal("SubvolSnapshot not recorded in call log")
	}
}

func TestMockCallLog(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol1")
	if err != nil {
		t.Fatalf("SubvolCreate vol1: %v", err)
	}

	_, err = mock.SubvolInfo("vol1")
	if err != nil {
		t.Fatalf("SubvolInfo vol1: %v", err)
	}

	_, err = mock.SubvolList("vol1")
	if err != nil {
		t.Fatalf("SubvolList vol1: %v", err)
	}

	log := mock.GetLog()
	if len(log) != 3 {
		t.Fatalf("expected 3 call log entries, got %d", len(log))
	}

	expected := []string{"SubvolCreate", "SubvolInfo", "SubvolList"}
	for i, op := range expected {
		if log[i].Operation != op {
			t.Fatalf("log entry %d: expected %q, got %q", i, op, log[i].Operation)
		}
	}
}

func TestMockSubvolInfoNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	_, err := mock.SubvolInfo("nonexistent")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}
}

func TestMockSubvolIDNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	id, err := mock.SubvolID("nonexistent")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}

	if id != 0 {
		t.Fatalf("expected id 0 for nonexistent subvol, got %d", id)
	}
}

// --- Rename tests ---

func TestMockSubvolRename(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("old")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.SubvolRename("old", "new")
	if err != nil {
		t.Fatalf("SubvolRename: %v", err)
	}

	err = mock.IsSubvolume("new")
	if err != nil {
		t.Fatalf("expected renamed volume to exist: %v", err)
	}

	err = mock.IsSubvolume("old")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected old name to not exist, got: %v", err)
	}
}

func TestMockSubvolRenameNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolRename("nonexistent", "new")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got: %v", err)
	}
}

func TestMockSubvolRenamePreservesQuota(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 1024)
	if err != nil {
		t.Fatalf("QGroupLimit: %v", err)
	}

	err = mock.SubvolRename("vol", "renamed")
	if err != nil {
		t.Fatalf("SubvolRename: %v", err)
	}

	if mock.Quotas["renamed"] != 1024 {
		t.Fatalf("expected quota 1024 on renamed vol, got %d", mock.Quotas["renamed"])
	}

	if _, ok := mock.Quotas["vol"]; ok {
		t.Fatal("old quota entry should be removed")
	}
}

// --- QGroupLimit tests ---

func TestMockQGroupLimit(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 4096)
	if err != nil {
		t.Fatalf("QGroupLimit: %v", err)
	}

	if mock.Quotas["vol"] != 4096 {
		t.Fatalf("expected quota 4096, got %d", mock.Quotas["vol"])
	}
}

func TestMockQGroupLimitZeroRemoves(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 4096)
	if err != nil {
		t.Fatalf("QGroupLimit set: %v", err)
	}

	err = mock.QGroupLimit("vol", 0)
	if err != nil {
		t.Fatalf("QGroupLimit clear: %v", err)
	}

	if _, ok := mock.Quotas["vol"]; ok {
		t.Fatal("expected quota to be removed for 0")
	}
}

func TestMockQGroupShow(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 4096)
	if err != nil {
		t.Fatalf("QGroupLimit: %v", err)
	}

	val, err := mock.QGroupShow("vol")
	if err != nil {
		t.Fatalf("QGroupShow: %v", err)
	}
	if val != 4096 {
		t.Fatalf("expected 4096, got %d", val)
	}

	val, err = mock.QGroupShow("nonexistent")
	if err != nil {
		t.Fatalf("QGroupShow nonexistent: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for missing, got %d", val)
	}
}
