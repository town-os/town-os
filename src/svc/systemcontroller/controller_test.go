package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestSystemControllerStorage(t *testing.T) {
	mock := storage.InitBtrFSMock()
	controller := mock.Controller.(*storage.MockBtrFSController)
	ts := InitTestServer(mock)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Could not create client: %v", err)
	}

	if err := c.CreateFilesystem("test"); err != nil {
		t.Fatalf("Could not create filesystem: %v", err)
	}

	info := controller.GetFilesystems()
	if len(info) != 1 {
		t.Fatalf("Filesystem was not recorded, len should be 1: %d", len(info))
	}

	if err := c.RemoveFilesystem("test"); err != nil {
		t.Fatalf("Could not create filesystem: %v", err)
	}

	if len(controller.GetFilesystems()) != 0 {
		t.Fatalf("Filesystem was not removed, len should be 0: %d", len(info))
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("Filesystem was not recorded, len should be 0: %d", len(fs))
	}
}

func TestSystemControllerListFilesystems(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Could not create client: %v", err)
	}

	for _, name := range []string{"vol-a", "vol-b", "vol-c"} {
		if err := c.CreateFilesystem(name); err != nil {
			t.Fatalf("Could not create filesystem %q: %v", name, err)
		}
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error listing filesystems: %v", err)
	}

	if len(fs) != 3 {
		t.Fatalf("Expected 3 filesystems, got %d", len(fs))
	}

	// List with prefix filter
	fs, err = c.ListFilesystems("vol-a")
	if err != nil {
		t.Fatalf("Error listing filesystems with prefix: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("Expected 1 filesystem with prefix vol-a, got %d", len(fs))
	}
}

func TestSystemControllerCreateAndRemoveMultiple(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(mock)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Could not create client: %v", err)
	}

	// Create two, remove one, verify the other remains
	if err := c.CreateFilesystem("keep"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateFilesystem("remove"); err != nil {
		t.Fatal(err)
	}

	if err := c.RemoveFilesystem("remove"); err != nil {
		t.Fatal(err)
	}

	fs, err := c.ListFilesystems("")
	if err != nil {
		t.Fatal(err)
	}

	if len(fs) != 1 {
		t.Fatalf("Expected 1 filesystem after removal, got %d", len(fs))
	}

	if fs[0].Name != "keep" {
		t.Fatalf("Expected remaining filesystem to be 'keep', got %q", fs[0].Name)
	}
}
