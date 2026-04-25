package storage

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNoFilesystem = errors.New("invalid filesystem")
var ErrUnimplemented = errors.New("unimplemented call")
var ErrRootFilesystem = errors.New("cannot modify root filesystem")
var ErrReservedFilesystem = errors.New("cannot modify reserved filesystem")
var ErrInvalidName = errors.New("invalid filesystem name")
var ErrPackageVolumeRename = errors.New("cannot rename package volume")

type Filesystem struct {
	Name         string `json:"name"`
	InternalName string `json:"internal_name,omitempty"`
	Quota        uint64 `json:"quota"`
	State        string `json:"state,omitempty"`
}

type SubvolInfo struct {
	Name string
	ID   uint64
}

type DiskUsage struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}

type Storage interface {
	CreateFilesystem(Filesystem) error
	ModifyFilesystem(string, Filesystem) error
	RemoveFilesystem(string) error
	ListFilesystems(string) ([]Filesystem, error)
	// FilesystemNames returns just the names of every subvolume matching
	// prefix. Unlike ListFilesystems it does not query per-subvolume quota,
	// so it is the right call for hot paths (e.g. /status/ping) that only
	// need to count or classify by name.
	FilesystemNames(prefix string) ([]string, error)
	RenameFilesystem(oldName, newName string) error
	SnapshotFilesystem(src, dst string) error
	DiskUsage() (DiskUsage, error)
}

type Controller interface {
	IsSubvolume(string) error
	SubvolCreate(string) error
	SubvolDelete(string) error
	SubvolID(string) (uint64, error)
	SubvolSnapshot(string, string, bool) error
	SubvolInfo(string) (SubvolInfo, error)
	SubvolList(string) ([]SubvolInfo, error)
	SubvolRename(oldPath, newPath string) error
	QuotaEnable(path string) error
	QGroupLimit(path string, bytes uint64) error
	QGroupShow(path string) (uint64, error)
}

// ValidateFilesystemName checks that a name is a valid unix path component
// with no leading slashes, no path traversal, and only safe characters.
func ValidateFilesystemName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidName)
	}

	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: name must not start with a slash", ErrInvalidName)
	}

	if strings.Contains(name, "\x00") {
		return fmt.Errorf("%w: name must not contain null bytes", ErrInvalidName)
	}

	for part := range strings.SplitSeq(name, "/") {
		if part == "" {
			return fmt.Errorf("%w: name must not contain empty path components", ErrInvalidName)
		}
		if part == "." || part == ".." {
			return fmt.Errorf("%w: name must not contain path traversal", ErrInvalidName)
		}
		for _, ch := range part {
			if !isValidNameChar(ch) {
				return fmt.Errorf("%w: name contains invalid character %q", ErrInvalidName, string(ch))
			}
		}
	}

	return nil
}

func isValidNameChar(ch rune) bool {
	if ch >= 'a' && ch <= 'z' {
		return true
	}
	if ch >= 'A' && ch <= 'Z' {
		return true
	}
	if ch >= '0' && ch <= '9' {
		return true
	}
	return ch == '-' || ch == '_' || ch == '.'
}
