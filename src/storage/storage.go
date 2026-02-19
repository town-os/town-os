package storage

import (
	"errors"

	"github.com/containerd/btrfs/v2"
)

var ErrNoFilesystem = errors.New("invalid filesystem")
var ErrUnimplemented = errors.New("unimplemented call")
var ErrRootFilesystem = errors.New("cannot modify root filesystem")

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
}
