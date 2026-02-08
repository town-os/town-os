package storage

import (
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestBtrFS(t *testing.T) {
	btr := storage.BtrFSDefault()

	if err := btr.NewFilesystem(storage.Filesystem{Name: "../local-mount/test"}); err != nil {
		t.Fatalf("Could not create filesystem test: %v", err)
	}
}
