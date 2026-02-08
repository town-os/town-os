//go:build integration

package storage

import "testing"

func TestBtrFS(t *testing.T) {
	btr := BtrFSDefault()

	if err := btr.NewFilesystem(Filename{Name: "test"}); err != nil {
		t.Fatalf("Could not create filesystem test: %v", err)
	}
}
