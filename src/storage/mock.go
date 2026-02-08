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

type MockController struct {
	Lock        *sync.Mutex
	Call        []Call
	NextID      uint64
	Filesystems []btrfs.Info
}

func InitMock() *MockController {
	return &MockController{Lock: new(sync.Mutex), Call: []Call{}, Filesystems: []btrfs.Info{}, NextID: 0}
}

func (m *MockController) GetLog() []Call {
	defer m.Lock.Unlock()
	m.Lock.Lock()
	return m.Call
}

func (m *MockController) GetFilesystems() []btrfs.Info {
	defer m.Lock.Unlock()
	m.Lock.Lock()
	return m.Filesystems
}

func (m *MockController) addCall(op string, err error, args ...any) {
	defer m.Lock.Unlock()
	m.Lock.Lock()

	m.Call = append(m.Call, Call{Operation: op, Arguments: args, Error: err})
}

func (m *MockController) addFilesystem(name string) {
	defer m.Lock.Unlock()
	m.Lock.Lock()

	m.NextID += 1
	m.Filesystems = append(m.Filesystems, btrfs.Info{Name: name, ID: m.NextID})
}

func (m *MockController) removeFilesystem(name string) {
	defer m.Lock.Unlock()
	m.Lock.Lock()

	fs := []btrfs.Info{}

	for _, f := range m.Filesystems {
		if f.Name != name {
			fs = append(fs, f)
		}
	}

	m.Filesystems = fs
}

func (m *MockController) IsSubvolume(name string) error {
	defer m.Lock.Unlock()
	m.Lock.Lock()

	var err error = nil

	for _, fs := range m.Filesystems {
		if fs.Name == name {
			err = nil
			break
		}
	}

	err = ErrNoFilesystem

	m.addCall("IsSubvolume", err, name)
	return err
}

func (m *MockController) SubvolCreate(name string) error {
	m.addFilesystem(name)
	m.addCall("SubvolCreate", nil, name)
	return nil
}

func (m *MockController) SubvolDelete(name string) error {
	list, err := m.SubvolList(name)

	if err == nil {
		for _, info := range list {
			m.removeFilesystem(info.Name)
		}
	}

	m.addCall("SubvolDelete", err, name)
	return nil
}

func (m *MockController) SubvolID(name string) (uint64, error) {
	var id uint64 = 0
	var err error = nil

	if info, merr := m.SubvolInfo(name); merr != nil {
		err = merr
	} else {
		id = info.ID
	}

	if id != 0 && err == nil {
		return id, err
	}

	m.addCall("SubvolID", err, id)
	return id, err
}

func (m *MockController) SubvolInfo(name string) (btrfs.Info, error) {
	var info = new(btrfs.Info)
	var err error = nil
	for _, fs := range m.Filesystems {
		if fs.Name == name {
			info = &fs
		}
	}

	if info == nil {
		err = ErrNoFilesystem
	}

	m.addCall("SubvolInfo", err, name)
	return *info, err
}

func (m *MockController) SubvolSnapshot(dst, src string, readonly bool) error {
	m.addCall("SubvolSnapshot", nil, dst, src, readonly)
	return nil
}

func (m *MockController) SubvolList(name string) ([]btrfs.Info, error) {
	// FIXME: I doubt this works right

	info := []btrfs.Info{}

	for _, fs := range m.Filesystems {
		if strings.HasPrefix(fs.Name, name) {
			info = append(info, fs)
		}
	}

	m.addCall("SubvolList", nil, info)
	return info, nil
}
