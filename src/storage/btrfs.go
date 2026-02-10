package storage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/btrfs/v2"
)

var ErrMountNotFound = errors.New("mount point not found")

func findMountPoint(path string) (string, error) {
	fp, err := os.Open("/proc/self/mounts")
	if err != nil {
		return "", err
	}
	defer fp.Close()

	const (
		deviceIdx = 0
		pathIdx   = 1
		typeIdx   = 2
		options   = 3
	)

	var (
		mount   string
		scanner = bufio.NewScanner(fp)
	)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if fields[typeIdx] != "btrfs" {
			continue // skip non-btrfs
		}

		if strings.HasPrefix(path, fields[pathIdx]) {
			mount = fields[pathIdx]
		}
	}

	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	if mount == "" {
		return "", ErrMountNotFound
	}

	return mount, nil
}

type BtrFSController struct{}

func (BtrFSController) IsSubvolume(name string) error {
	return btrfs.IsSubvolume(name)
}

func (BtrFSController) SubvolCreate(name string) error { return btrfs.SubvolCreate(name) }

func (BtrFSController) SubvolDelete(name string) error { return btrfs.SubvolDelete(name) }

func (BtrFSController) SubvolID(name string) (uint64, error) { return btrfs.SubvolID(name) }

func (BtrFSController) SubvolSnapshot(dst, src string, readonly bool) error {
	return btrfs.SubvolSnapshot(dst, src, readonly)
}

func (BtrFSController) SubvolInfo(name string) (btrfs.Info, error) { return btrfs.SubvolInfo(name) }

func (BtrFSController) SubvolList(name string) ([]btrfs.Info, error) { return btrfs.SubvolList(name) }

type BtrFS struct {
	BinPath    string
	Controller Controller
}

func InitBtrFS() *BtrFS {
	return InitBtrFSFromController(BtrFSController{})
}

func InitBtrFSMock() *BtrFS {
	return InitBtrFSFromController(InitBtrFSMockController())
}

func InitBtrFSFromController(c Controller) *BtrFS {
	return &BtrFS{
		BinPath:    "btrfs",
		Controller: c,
	}
}

func (b *BtrFS) CreateFilesystem(f Filesystem) error {
	return b.Controller.SubvolCreate(f.Name)
}

func (b *BtrFS) ModifyFilesystem(name string, f Filesystem) error {
	return ErrUnimplemented
}

func (b *BtrFS) RemoveFilesystem(name string) error {
	return b.Controller.SubvolDelete(name)
}

func (b *BtrFS) ListFilesystems(prefix string) ([]Filesystem, error) {
	mnt, err := findMountPoint(prefix)
	if err != nil {
		return nil, err
	}

	info, err := b.Controller.SubvolList(prefix)
	if err != nil {
		return nil, err
	}

	p, err := filepath.Abs(prefix)
	if err != nil {
		return nil, fmt.Errorf("could not determine absolute path of prefix: %v", err)
	}

	s, err := filepath.Rel(mnt, p)
	if err != nil {
		return nil, fmt.Errorf("could not determine relative path: %v", err)
	}

	if s == "." {
		s = ""
	}

	uniq := map[string]struct{}{}
	fs := []Filesystem{}

	for _, item := range info {
		if s == "" || (s != "" && strings.HasPrefix(item.Name, s)) {
			if _, ok := uniq[item.Name]; !ok {
				fs = append(fs, Filesystem{Name: item.Name})
				uniq[item.Name] = struct{}{}
			}
		}
	}

	return fs, nil
}
