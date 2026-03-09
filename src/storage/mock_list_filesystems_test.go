// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"testing"
)

func TestListFilesystemsReturnsQuota(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 2048})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Quota != 2048 {
		t.Fatalf("expected quota 2048, got %d", fs[0].Quota)
	}
}

func TestListFilesystemsPrefixFilter(t *testing.T) {
	mock := InitBtrFSMock()

	// Create volumes for two packages with similar-prefix names.
	for _, name := range []string{"nginx/html", "nginx/logs", "nginx2/data"} {
		err := mock.CreateFilesystem(Filesystem{Name: name})
		if err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// List with trailing-slash prefix: should return only nginx children, not
	// the parent "nginx" itself and not "nginx2" or its children.
	fs, err := mock.ListFilesystems("nginx/")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	names := map[string]bool{}
	for _, f := range fs {
		names[f.Name] = true
	}

	if !names["nginx/html"] {
		t.Fatal("expected nginx/html in prefix-filtered list")
	}
	if !names["nginx/logs"] {
		t.Fatal("expected nginx/logs in prefix-filtered list")
	}
	if names["nginx"] {
		t.Fatal("parent nginx should not appear when filtering by nginx/")
	}
	if names["nginx2"] || names["nginx2/data"] {
		t.Fatal("nginx2 volumes should not appear when filtering by nginx/")
	}
}
