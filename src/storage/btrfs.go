package storage

import "github.com/containerd/btrfs/v2"

type BtrFSController struct{}

func (BtrFSController) IsSubvolume(name string) error {
	return btrfs.IsSubvolume(name)
}

func (BtrFSController) SubvolCreate(name string) error       { return btrfs.SubvolCreate(name) }
func (BtrFSController) SubvolDelete(name string) error       { return btrfs.SubvolDelete(name) }
func (BtrFSController) SubvolID(name string) (uint64, error) { return btrfs.SubvolID(name) }
func (BtrFSController) SubvolSnapshot(dst, src string, readonly bool) error {
	return btrfs.SubvolSnapshot(dst, src, readonly)
}
func (BtrFSController) SubvolInfo(name string) (btrfs.Info, error)   { return btrfs.SubvolInfo(name) }
func (BtrFSController) SubvolList(name string) ([]btrfs.Info, error) { return btrfs.SubvolList(name) }

type BtrFS struct {
	BinPath    string
	Controller Controller
}

func BtrFSDefault() *BtrFS {
	return &BtrFS{
		BinPath: "btrfs",
		// FIXME finish
		Controller: BtrFSController{},
	}
}

func BtrFSMock() *BtrFS {
	return &BtrFS{
		BinPath:    "btrfs",
		Controller: new(MockController),
	}
}

func InitBtrFS(c Controller) *BtrFS {
	return &BtrFS{
		BinPath:    "btrfs",
		Controller: c,
	}
}

func (b *BtrFS) NewFilesystem(f Filesystem) error {
	return b.Controller.SubvolCreate(f.Name)
}

func (b *BtrFS) ModifyFilesystem(name string, f Filesystem) error {
	return ErrUnimplemented
}

func (b *BtrFS) RemoveFilesystem(f Filesystem) error {
	return b.Controller.SubvolDelete(f.Name)
}

func (b *BtrFS) ListFilesystems(prefix string) ([]Filesystem, error) {
	info, err := b.Controller.SubvolList(prefix)
	if err != nil {
		return nil, err
	}

	fs := []Filesystem{}

	for _, item := range info {
		fs = append(fs, Filesystem{Name: item.Name})
	}

	return fs, nil
}
