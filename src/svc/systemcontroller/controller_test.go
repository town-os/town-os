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

	info = controller.GetFilesystems()
	if len(info) != 0 {
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
