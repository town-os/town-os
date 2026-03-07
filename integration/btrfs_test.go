package integration_test

import (
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestBtrFS(t *testing.T) {
	btr := storage.InitBtrFS("/town-os")

	baseList, err := btr.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error while listing filesystems before create: %v", err)
	}
	baseCount := len(baseList)

	err = btr.CreateFilesystem(storage.Filesystem{Name: "test"})
	if err != nil {
		t.Fatalf("Could not create filesystem test: %v", err)
	}

	list, err := btr.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != baseCount+1 {
		t.Fatalf("Expected %d filesystems after create, got %d", baseCount+1, len(list))
	}

	list, err = btr.ListFilesystems("test")
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Expected 1 filesystem under test path, got %d", len(list))
	}

	err = btr.RemoveFilesystem("test")
	if err != nil {
		t.Fatalf("Could not remove filesystem test: %v", err)
	}

	list, err = btr.ListFilesystems("")
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != baseCount {
		t.Fatalf("Expected %d filesystems after remove, got %d", baseCount, len(list))
	}
}
