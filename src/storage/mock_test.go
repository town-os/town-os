package storage

import "testing"

func TestMockBtrFSStorage(t *testing.T) {
	mock := InitBtrFSMock()
	if err := mock.CreateFilesystem(Filesystem{Name: "test"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("Len should be 1 and was not: %d", len(fs))
	}

	if err := mock.RemoveFilesystem("test"); err != nil {
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

	if err := mock.SubvolCreate("test"); err != nil {
		t.Fatalf("SubvolCreate test: %v", err)
	}

	if err := mock.SubvolCreate("test/sub"); err != nil {
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

	info, err := mock.SubvolList("test")
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

	if err := mock.SubvolDelete("test/sub"); err != nil {
		t.Fatalf("SubvolDelete test/sub: %v", err)
	}

	info, err = mock.SubvolList("test/sub")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test/sub` was not deleted after delete call")
	}

	if err := mock.SubvolDelete("test"); err != nil {
		t.Fatalf("SubvolDelete test: %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}

	if err := mock.SubvolCreate("test"); err != nil {
		t.Fatalf("SubvolCreate test (re-create): %v", err)
	}

	if err := mock.SubvolCreate("test/sub"); err != nil {
		t.Fatalf("SubvolCreate test/sub (re-create): %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 2 {
		t.Fatal("Volumes were not re-created")
	}

	if err := mock.SubvolDelete("test"); err != nil {
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

	if err := mock.IsSubvolume("nonexistent"); err != ErrNoFilesystem {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}

	if err := mock.SubvolCreate("test"); err != nil {
		t.Fatalf("SubvolCreate test: %v", err)
	}

	if err := mock.IsSubvolume("test"); err != nil {
		t.Fatalf("expected nil error for existing subvolume, got %v", err)
	}

	if err := mock.IsSubvolume("test/sub"); err != ErrNoFilesystem {
		t.Fatalf("expected ErrNoFilesystem for non-existing sub, got %v", err)
	}
}

func TestMockSubvolSnapshot(t *testing.T) {
	mock := InitBtrFSMockController()

	if err := mock.SubvolCreate("src"); err != nil {
		t.Fatalf("SubvolCreate src: %v", err)
	}

	if err := mock.SubvolSnapshot("dst", "src", true); err != nil {
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

	if err := mock.SubvolCreate("vol1"); err != nil {
		t.Fatalf("SubvolCreate vol1: %v", err)
	}

	if _, err := mock.SubvolInfo("vol1"); err != nil {
		t.Fatalf("SubvolInfo vol1: %v", err)
	}

	if _, err := mock.SubvolList("vol1"); err != nil {
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
	if err != ErrNoFilesystem {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}
}

func TestMockSubvolIDNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	id, err := mock.SubvolID("nonexistent")
	if err != ErrNoFilesystem {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}

	if id != 0 {
		t.Fatalf("expected id 0 for nonexistent subvol, got %d", id)
	}
}
