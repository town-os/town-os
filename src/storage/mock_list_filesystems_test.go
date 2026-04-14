// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"errors"
	"strings"
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

// TestListFilesystemsSkipsConcurrentlyDeletedSubvol covers the race where a
// parallel test (or real reconcile) removes a subvolume between SubvolList
// enumerating it and QGroupShow running against it. ListFilesystems must
// skip the vanished entry instead of surfacing the btrfs "can't access" (or
// equivalent) error and failing an otherwise-correct caller. This is the
// failure that TestBtrFS hit in make test-full when running alongside the
// sc-mnq* filesystem tests.
func TestListFilesystemsSkipsConcurrentlyDeletedSubvol(t *testing.T) {
	for _, tc := range []struct {
		label  string
		errMsg string
	}{
		{"can't access", "btrfs subvolume show: exit status 1\nERROR: can't access '/town-os/user/gone'"},
		{"cannot access", "btrfs subvolume show: exit status 1\nERROR: cannot access '/town-os/user/gone'"},
		{"No such file", "btrfs subvolume show: exit status 1\nERROR: No such file or directory"},
		{"not a subvolume", "btrfs subvolume show: exit status 1\nERROR: not a subvolume"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			mock := InitBtrFSMock()
			for _, name := range []string{"alive", "gone"} {
				if err := mock.CreateFilesystem(Filesystem{Name: name, Quota: 4096}); err != nil {
					t.Fatalf("CreateFilesystem %q: %v", name, err)
				}
			}

			ctrl, ok := mock.Controller.(*MockBtrFSController)
			if !ok {
				t.Fatalf("expected *MockBtrFSController, got %T", mock.Controller)
			}
			errToInject := errors.New(tc.errMsg) //nolint:err113 // simulating a real btrfs error payload string
			ctrl.QGroupShowFunc = func(path string) (uint64, error) {
				if strings.HasSuffix(path, "/gone") {
					return 0, errToInject
				}
				return 4096, nil
			}

			fs, err := mock.ListFilesystems("")
			if err != nil {
				t.Fatalf("ListFilesystems: %v", err)
			}
			if len(fs) != 1 {
				t.Fatalf("expected 1 filesystem (gone skipped), got %d: %+v", len(fs), fs)
			}
			if fs[0].Name != "alive" {
				t.Fatalf("expected alive, got %q", fs[0].Name)
			}
		})
	}
}

// TestListFilesystemsPropagatesRealErrors makes sure that a genuine quota
// lookup failure (not a vanished subvolume) still bubbles up to the caller,
// so we don't accidentally swallow real bugs along with the race fix.
func TestListFilesystemsPropagatesRealErrors(t *testing.T) {
	mock := InitBtrFSMock()
	if err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 4096}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	ctrl, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatalf("expected *MockBtrFSController, got %T", mock.Controller)
	}
	ctrl.QGroupShowFunc = func(_ string) (uint64, error) {
		return 0, errors.New("qgroup show: some other unexpected failure") //nolint:err113 // test-local sentinel
	}

	_, err := mock.ListFilesystems("")
	if err == nil {
		t.Fatal("expected error from ListFilesystems, got nil")
	}
	if !strings.Contains(err.Error(), "some other unexpected failure") {
		t.Fatalf("expected the underlying error to bubble up, got: %v", err)
	}
}
