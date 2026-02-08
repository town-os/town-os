package storage

type BtrFS struct {
	BinPath    string
	Controller Controller
}

func BtrFSDefault() *BtrFS {
	return &BtrFS{
		BinPath: "btrfs",
		// FIXME finish
		Controller: nil,
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

func (b *BtrFS) NewVolume(v Volume) error {
	return nil
}

func (b *BtrFS) NewFilesystem(f Filesystem) error {
	return nil
}

func (b *BtrFS) ModifyVolume(name string, v Volume) error {
	return nil
}

func (b *BtrFS) ModifyFilesystem(name string, f Filesystem) error {
	return nil
}

func (b *BtrFS) RemoveVolume(v Volume) error {
	return nil
}

func (b *BtrFS) RemoveFilesystem(f Filesystem) error {
	return nil
}

func (b *BtrFS) ListFilesystems() ([]Filesystem, error) {
	return []Filesystem{}, nil
}

func (b *BtrFS) ListVolumes() ([]Volume, error) {
	return []Volume{}, nil
}
