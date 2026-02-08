package storage

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

	btr := storage.BtrFSDefault()

	if err := btr.NewFilesystem(storage.Filesystem{Name: filepath.Join(path, "test")}); err != nil {
		t.Fatalf("Could not create filesystem test: %v", err)
	}

	list, err := btr.ListFilesystems(path)
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("Incorrect number of filesystems: %d", len(list))
	}

	list, err = btr.ListFilesystems(filepath.Join(path, "test"))
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Incorrect number of filesystems: %d", len(list))
	}

	if err := btr.RemoveFilesystem(filepath.Join(path, "test")); err != nil {
		t.Fatalf("Could not create filesystem test: %v", err)
	}

	list, err = btr.ListFilesystems(path)
	if err != nil {
		t.Fatalf("Error while listing filesystems: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Incorrect number of filesystems: %d", len(list))
	}
}
