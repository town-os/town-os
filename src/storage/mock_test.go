package storage

import "testing"

/*
FIXME

Finish testing mock calls
	- IsSubvolume
	- Snapshot stuff
Test call log
*/

func TestMockBtrFSBasic(t *testing.T) {
	mock := InitBtrFSMockController()

	if err := mock.SubvolCreate("test"); err != nil {
		t.Fatal(err)
	}

	if err := mock.SubvolCreate("test/sub"); err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}

	info, err = mock.SubvolList("test/sub")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test/sub` was not deleted after delete call")
	}

	if err := mock.SubvolDelete("test"); err != nil {
		t.Fatal(err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}

	if err := mock.SubvolCreate("test"); err != nil {
		t.Fatal(err)
	}

	if err := mock.SubvolCreate("test/sub"); err != nil {
		t.Fatal(err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 2 {
		t.Fatal("Volumes were not re-created")
	}

	if err := mock.SubvolDelete("test"); err != nil {
		t.Fatal(err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}
}
