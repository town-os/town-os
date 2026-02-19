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

func findMountPoint(path string) (_ string, err error) {
	fp, err := os.Open("/proc/self/mounts")
	if err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, fp.Close())
	}()

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
		if len(fields) <= typeIdx {
			continue // skip malformed lines
		}

		if fields[typeIdx] != "btrfs" {
			continue // skip non-btrfs
		}

		mp := fields[pathIdx]
		if path == mp || strings.HasPrefix(path, mp+"/") {
			mount = mp
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

func (BtrFSController) SubvolList(name string) ([]btrfs.Info, error) {
	mnt, err := findMountPoint(name)
	if err != nil {
		return nil, err
	}

	p, err := filepath.Abs(name)
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

	info, err := btrfs.SubvolList(name)
	if err != nil {
		return nil, err
	}

	uniq := map[string]struct{}{}
	fs := []btrfs.Info{}

	for _, item := range info {
		if s == "" || (s != "" && strings.HasPrefix(item.Name, s)) {
			if _, ok := uniq[item.Name]; !ok {
				fs = append(fs, item)
				uniq[item.Name] = struct{}{}
			}
		}
	}

	return fs, nil
}

type BtrFS struct {
	BasePath   string
	BinPath    string
	Controller Controller
}

func InitBtrFS(basePath string) *BtrFS {
	return InitBtrFSFromController(basePath, BtrFSController{})
}

func InitBtrFSMock() *BtrFS {
	return InitBtrFSFromController("", InitBtrFSMockController())
}

func InitBtrFSFromController(basePath string, c Controller) *BtrFS {
	return &BtrFS{
		BasePath:   basePath,
		BinPath:    "btrfs",
		Controller: c,
	}
}

func (b *BtrFS) CreateFilesystem(f Filesystem) error {
	return b.Controller.SubvolCreate(filepath.Join(b.BasePath, f.Name))
}

func (b *BtrFS) ModifyFilesystem(name string, f Filesystem) error {
	return ErrUnimplemented
}

func (b *BtrFS) RemoveFilesystem(name string) error {
	return b.Controller.SubvolDelete(filepath.Join(b.BasePath, name))
}

func (b *BtrFS) ListFilesystems(prefix string) ([]Filesystem, error) {
	info, err := b.Controller.SubvolList(filepath.Join(b.BasePath, prefix))
	if err != nil {
		return nil, err
	}

	fs := []Filesystem{}

	for _, item := range info {
		fs = append(fs, Filesystem{Name: item.Name})
	}

	return fs, nil
}
