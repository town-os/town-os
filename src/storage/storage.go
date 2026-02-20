package storage

import (
	"errors"
	"fmt"
	"strings"

	"github.com/containerd/btrfs/v2"
)

var ErrNoFilesystem = errors.New("invalid filesystem")
var ErrUnimplemented = errors.New("unimplemented call")
var ErrRootFilesystem = errors.New("cannot modify root filesystem")
var ErrInvalidName = errors.New("invalid filesystem name")

type Filesystem struct {
	Name  string `json:"name"`
	Quota uint64 `json:"quota"`
}

type Storage interface {
	CreateFilesystem(Filesystem) error
	ModifyFilesystem(string, Filesystem) error
	RemoveFilesystem(string) error
	ListFilesystems(string) ([]Filesystem, error)
}

type Controller interface {
	IsSubvolume(string) error
	SubvolCreate(string) error
	SubvolDelete(string) error
	SubvolID(string) (uint64, error)
	SubvolSnapshot(string, string, bool) error
	SubvolInfo(string) (btrfs.Info, error)
	SubvolList(string) ([]btrfs.Info, error)
	SubvolRename(oldPath, newPath string) error
	QGroupLimit(path string, bytes uint64) error
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

	for _, part := range strings.Split(name, "/") {
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
