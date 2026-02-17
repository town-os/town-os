package integration_test

import (
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestBtrFS(t *testing.T) {
	path, err := filepath.Abs("../local-mount")
	if err != nil {
		t.Fatalf("Could not find absolute path: %v", err)
	}

	btr := storage.InitBtrFS()

	baseList, err := btr.ListFilesystems(path)
	if err != nil {
		t.Fatalf("Error while listing filesystems before create: %v", err)
	}
	baseCount := len(baseList)

	testPath := filepath.Join(path, "test")

	if err := btr.CreateFilesystem(storage.Filesystem{Name: testPath}); err != nil {
		t.Fatalf("Could not create filesystem test: %v", err)
	}

	list, err := btr.ListFilesystems(path)
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != baseCount+1 {
		t.Fatalf("Expected %d filesystems after create, got %d", baseCount+1, len(list))
	}

	list, err = btr.ListFilesystems(testPath)
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Expected 1 filesystem under test path, got %d", len(list))
	}

	if err := btr.RemoveFilesystem(testPath); err != nil {
		t.Fatalf("Could not remove filesystem test: %v", err)
	}

	list, err = btr.ListFilesystems(path)
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != baseCount {
		t.Fatalf("Expected %d filesystems after remove, got %d", baseCount, len(list))
	}
}
