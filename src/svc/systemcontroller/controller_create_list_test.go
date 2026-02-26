package systemcontroller

import (
	"context"
	"fmt"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestCreateListRemoveLifecycle(t *testing.T) {
	c, _ := initTestClient(t)

	// Start empty
	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems (initial): %v", err)
	}
	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected empty list, got %d", len(fsResult.Entries))
	}

	// Create
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "lifecycle-vol"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "lifecycle-vol", err)
	}

	// Verify present
	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems (after create): %v", err)
	}
	if len(fsResult.Entries) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fsResult.Entries))
	}
	if fsResult.Entries[0].Name != "lifecycle-vol" {
		t.Fatalf("expected name %q, got %q", "lifecycle-vol", fsResult.Entries[0].Name)
	}

	// Remove
	if err := c.RemoveFilesystem(context.TODO(), "lifecycle-vol"); err != nil {
		t.Fatalf("RemoveFilesystem %q: %v", "lifecycle-vol", err)
	}

	// Verify gone
	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems (after remove): %v", err)
	}
	if len(fsResult.Entries) != 0 {
		t.Fatalf("expected 0 after removal, got %d", len(fsResult.Entries))
	}
}

func TestBulkCreateAndRemove(t *testing.T) {
	c, _ := initTestClient(t)

	count := 10
	for i := range count {
		name := fmt.Sprintf("vol-%d", i)
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems (after bulk create): %v", err)
	}
	if len(fsResult.Entries) != count {
		t.Fatalf("expected %d filesystems, got %d", count, len(fsResult.Entries))
	}

	// Remove evens
	for i := 0; i < count; i += 2 {
		name := fmt.Sprintf("vol-%d", i)
		if err := c.RemoveFilesystem(context.TODO(), name); err != nil {
			t.Fatalf("RemoveFilesystem %q: %v", name, err)
		}
	}

	fsResult, err = c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems (after bulk remove): %v", err)
	}
	if len(fsResult.Entries) != count/2 {
		t.Fatalf("expected %d filesystems after removal, got %d", count/2, len(fsResult.Entries))
	}
}
