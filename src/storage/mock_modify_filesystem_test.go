package storage

import (
	"testing"
)

func TestModifyFilesystemRename(t *testing.T) {
	mock := InitBtrFSMock()
	err := mock.CreateFilesystem(Filesystem{Name: "old"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("old", Filesystem{Name: "new", Quota: 0})
	if err != nil {
		t.Fatalf("ModifyFilesystem rename: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "new" {
		t.Fatalf("expected name %q, got %q", "new", fs[0].Name)
	}
}

func TestModifyFilesystemQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "vol"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "vol", Quota: 2048})
	if err != nil {
		t.Fatalf("ModifyFilesystem quota: %v", err)
	}

	if controller.Quotas["vol"] != 2048 {
		t.Fatalf("expected quota 2048, got %d", controller.Quotas["vol"])
	}
}

func TestModifyFilesystemRenameAndQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "old"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("old", Filesystem{Name: "new", Quota: 4096})
	if err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 || fs[0].Name != "new" {
		t.Fatalf("expected filesystem named %q, got %v", "new", fs)
	}

	if controller.Quotas["new"] != 4096 {
		t.Fatalf("expected quota 4096 on new name, got %d", controller.Quotas["new"])
	}
}

func TestModifyFilesystemInvalidName(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "vol"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "/bad", Quota: 0})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "..", Quota: 0})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "", Quota: 0})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestModifyFilesystemClearQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 1024})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	callsBefore := len(controller.GetLog())

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "vol", Quota: 0})
	if err != nil {
		t.Fatalf("ModifyFilesystem clear quota: %v", err)
	}

	calls := controller.GetLog()[callsBefore:]
	foundQGroupShow := false
	foundQGroupLimit := false
	for _, c := range calls {
		if c.Operation == "QGroupShow" {
			foundQGroupShow = true
		}
		if c.Operation == "QGroupLimit" {
			foundQGroupLimit = true
			if c.Arguments[1] != uint64(0) {
				t.Fatalf("expected QGroupLimit called with 0, got %v", c.Arguments[1])
			}
		}
	}
	if !foundQGroupShow {
		t.Fatal("expected QGroupShow call when clearing quota")
	}
	if !foundQGroupLimit {
		t.Fatal("expected QGroupLimit call with 0 when clearing quota")
	}
}
