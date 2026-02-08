package storage

import (
	"errors"

	"github.com/containerd/btrfs/v2"
)

var ErrNoFilesystem = errors.New("invalid filesystem")

type Filesystem struct {
	Name string `json:"name"`
	Size uint64 `json:"size"`
}

type Volume struct {
	Name       string  `json:"name"`
	Size       uint64  `json:"size"`
	Filesystem *string `json:"filesystem"`
}

type Storage interface {
	CreateFilesystem(Filesystem) error
	ModifyFilesystem(string, Filesystem) error
	RemoveFilesystem(string) error
	ListFilesystems() ([]Filesystem, error)
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
