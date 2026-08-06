package storage

import (
	"errors"
	"testing"
)

// CreateFilesystem, RenameFilesystem, and SnapshotFilesystem all validated
// their names; RemoveFilesystem — the destructive one — did not, and
// ModifyFilesystem validated only the name it was renaming TO. Both compose
// their argument with filepath.Join, which collapses "..", so an unvalidated
// name reaches any subvolume on the filesystem regardless of the prefix a
// caller was confined to.

func traversalNames() []string {
	return []string{
		"../escape",
		"user/../../etc",
		"user/../gfeh/home",
		"/absolute",
		"",
		"user//empty-component",
		"user/./dot",
		"user/nul\x00byte",
	}
}

func TestRemoveFilesystemValidatesName(t *testing.T) {
	for _, name := range traversalNames() {
		t.Run(name, func(t *testing.T) {
			b := InitBtrFSMock()
			err := b.RemoveFilesystem(name)
			if err == nil {
				t.Fatalf("RemoveFilesystem(%q) = nil, want ErrInvalidName", name)
			}
			if !errors.Is(err, ErrInvalidName) {
				t.Fatalf("RemoveFilesystem(%q) error = %v, want ErrInvalidName", name, err)
			}
		})
	}
}

func TestModifyFilesystemValidatesSourceName(t *testing.T) {
	for _, name := range traversalNames() {
		t.Run(name, func(t *testing.T) {
			b := InitBtrFSMock()
			err := b.ModifyFilesystem(name, Filesystem{Name: "user/target"})
			if err == nil {
				t.Fatalf("ModifyFilesystem(%q, ...) = nil, want ErrInvalidName", name)
			}
			if !errors.Is(err, ErrInvalidName) {
				t.Fatalf("ModifyFilesystem(%q, ...) error = %v, want ErrInvalidName", name, err)
			}
		})
	}
}

func TestRemoveFilesystemAcceptsValidNames(t *testing.T) {
	// The names the rest of the system actually removes: user volumes, package
	// volumes (including the dep form and dotted versions), pages directories
	// named after an FQDN, and object-storage partitions.
	valid := []string{
		"user/photos",
		"installed/core/gitea/1.0/data",
		"installed/core/jitsi--dep--prosody/2.1.0/config",
		"uninstalled/core/gitea/1.0/data",
		"pages/blog.home",
		"gfeh/home",
		"vm-images",
	}

	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			b := InitBtrFSMock()
			if err := b.CreateFilesystem(Filesystem{Name: name}); err != nil {
				t.Fatalf("CreateFilesystem(%q): %v", name, err)
			}
			if err := b.RemoveFilesystem(name); err != nil {
				t.Fatalf("RemoveFilesystem(%q): %v", name, err)
			}
		})
	}
}
