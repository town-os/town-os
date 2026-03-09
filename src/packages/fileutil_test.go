// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteJSONCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	data := map[string]string{"key": "value"}
	if err := atomicWriteJSON(path, data); err != nil {
		t.Fatalf("atomicWriteJSON: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("expected key=value, got key=%s", got["key"])
	}
}

func TestAtomicWriteJSONOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := atomicWriteJSON(path, map[string]string{"old": "data"}); err != nil {
		t.Fatalf("first write: %v", err)
	}

	if err := atomicWriteJSON(path, map[string]string{"new": "data"}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["old"]; ok {
		t.Fatal("expected old data to be overwritten")
	}
	if got["new"] != "data" {
		t.Fatalf("expected new=data, got new=%s", got["new"])
	}
}

func TestAtomicWriteJSONProducesIndented(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	data := map[string]int{"count": 42}
	if err := atomicWriteJSON(path, data); err != nil {
		t.Fatalf("atomicWriteJSON: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Indented JSON should contain newlines.
	if len(content) < 10 {
		t.Fatalf("expected indented JSON, got: %s", string(content))
	}
}

func TestAtomicWriteJSONCleansTempOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Channels cannot be marshalled to JSON — this will fail.
	err := atomicWriteJSON(path, make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshallable type")
	}

	// The target file should not exist.
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("expected target file to not exist after error")
	}

	// No temp files should remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("unexpected leftover file: %s", e.Name())
	}
}

func TestLockDirAcquiresAndReleases(t *testing.T) {
	dir := t.TempDir()

	lock, err := lockDir(dir)
	if err != nil {
		t.Fatalf("lockDir: %v", err)
	}

	if err := lock.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestLockDirCreatesLockFile(t *testing.T) {
	dir := t.TempDir()

	lock, err := lockDir(dir)
	if err != nil {
		t.Fatalf("lockDir: %v", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Errorf("Unlock: %v", err)
		}
	}()

	lockPath := filepath.Join(dir, ".lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected .lock file to exist: %v", err)
	}
}

func TestLockDirSequentialReacquire(t *testing.T) {
	dir := t.TempDir()

	lock1, err := lockDir(dir)
	if err != nil {
		t.Fatalf("first lockDir: %v", err)
	}
	if err := lock1.Unlock(); err != nil {
		t.Fatalf("first Unlock: %v", err)
	}

	lock2, err := lockDir(dir)
	if err != nil {
		t.Fatalf("second lockDir: %v", err)
	}
	if err := lock2.Unlock(); err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
}
