package storage

import (
	"strings"
	"sync"

	"github.com/containerd/btrfs/v2"
)

/*
FIXME:

- Parent ID
*/

type Call struct {
	Operation string
	Arguments []any
	Error     error
}

type MockBtrFSController struct {
	Lock        *sync.Mutex
	Call        []Call
	NextID      uint64
	Filesystems []btrfs.Info
}

func InitBtrFSMockController() *MockBtrFSController {
	return &MockBtrFSController{Lock: new(sync.Mutex), Call: []Call{}, Filesystems: []btrfs.Info{}, NextID: 0}
}

func (m *MockBtrFSController) GetLog() []Call {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	return m.Call
}

func (m *MockBtrFSController) GetFilesystems() []btrfs.Info {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	return m.Filesystems
}

func (m *MockBtrFSController) addCallLocked(op string, err error, args ...any) {
	m.Call = append(m.Call, Call{Operation: op, Arguments: args, Error: err})
}

func (m *MockBtrFSController) addFilesystemLocked(name string) {
	m.NextID += 1
	m.Filesystems = append(m.Filesystems, btrfs.Info{Name: name, ID: m.NextID})
}

func (m *MockBtrFSController) removeFilesystemLocked(name string) {
	fs := []btrfs.Info{}

	for _, f := range m.Filesystems {
		if f.Name != name {
			fs = append(fs, f)
		}
	}

	m.Filesystems = fs
}

func (m *MockBtrFSController) IsSubvolume(name string) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	var err error
	found := false

	for _, fs := range m.Filesystems {
		if fs.Name == name {
			found = true
			break
		}
	}

	if !found {
		err = ErrNoFilesystem
	}

	m.addCallLocked("IsSubvolume", err, name)
	return err
}

func (m *MockBtrFSController) SubvolCreate(name string) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	m.addFilesystemLocked(name)
	m.addCallLocked("SubvolCreate", nil, name)
	return nil
}

func (m *MockBtrFSController) SubvolDelete(name string) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	list := m.subvolListLocked(name)

	for _, info := range list {
		m.removeFilesystemLocked(info.Name)
	}

	m.addCallLocked("SubvolDelete", nil, name)
	return nil
}

func (m *MockBtrFSController) SubvolID(name string) (uint64, error) {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	info, err := m.subvolInfoLocked(name)

	m.addCallLocked("SubvolID", err, info.ID)
	return info.ID, err
}

func (m *MockBtrFSController) subvolInfoLocked(name string) (btrfs.Info, error) {
	for _, fs := range m.Filesystems {
		if fs.Name == name {
			return fs, nil
		}
	}

	return btrfs.Info{}, ErrNoFilesystem
}

func (m *MockBtrFSController) SubvolInfo(name string) (btrfs.Info, error) {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	info, err := m.subvolInfoLocked(name)
	m.addCallLocked("SubvolInfo", err, name)
	return info, err
}

func (m *MockBtrFSController) SubvolSnapshot(dst, src string, readonly bool) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	m.addCallLocked("SubvolSnapshot", nil, dst, src, readonly)
	return nil
}

func (m *MockBtrFSController) subvolListLocked(name string) []btrfs.Info {
	info := []btrfs.Info{}

	for _, fs := range m.Filesystems {
		if strings.HasPrefix(fs.Name, name) {
			info = append(info, fs)
		}
	}

	return info
}

func (m *MockBtrFSController) SubvolList(name string) ([]btrfs.Info, error) {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	info := m.subvolListLocked(name)
	m.addCallLocked("SubvolList", nil, info)
	return info, nil
}
