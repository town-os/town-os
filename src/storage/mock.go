package storage

import (
	"strings"
	"sync"
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
	Filesystems []SubvolInfo
	Quotas      map[string]uint64
}

func InitBtrFSMockController() *MockBtrFSController {
	return &MockBtrFSController{Lock: new(sync.Mutex), Call: []Call{}, Filesystems: []SubvolInfo{}, NextID: 0, Quotas: map[string]uint64{}}
}

func (m *MockBtrFSController) GetLog() []Call {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	return m.Call
}

func (m *MockBtrFSController) GetFilesystems() []SubvolInfo {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	return m.Filesystems
}

func (m *MockBtrFSController) addCallLocked(op string, err error, args ...any) {
	m.Call = append(m.Call, Call{Operation: op, Arguments: args, Error: err})
}

func (m *MockBtrFSController) addFilesystemLocked(name string) {
	m.NextID += 1
	m.Filesystems = append(m.Filesystems, SubvolInfo{Name: name, ID: m.NextID})
}

func (m *MockBtrFSController) removeFilesystemLocked(name string) {
	fs := []SubvolInfo{}

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
		delete(m.Quotas, info.Name)
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

func (m *MockBtrFSController) subvolInfoLocked(name string) (SubvolInfo, error) {
	for _, fs := range m.Filesystems {
		if fs.Name == name {
			return fs, nil
		}
	}

	return SubvolInfo{}, ErrNoFilesystem
}

func (m *MockBtrFSController) SubvolInfo(name string) (SubvolInfo, error) {
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

func (m *MockBtrFSController) subvolListLocked(name string) []SubvolInfo {
	info := []SubvolInfo{}

	for _, fs := range m.Filesystems {
		if strings.HasPrefix(fs.Name, name) {
			info = append(info, fs)
		}
	}

	return info
}

func (m *MockBtrFSController) SubvolList(name string) ([]SubvolInfo, error) {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	info := m.subvolListLocked(name)
	m.addCallLocked("SubvolList", nil, info)
	return info, nil
}

func (m *MockBtrFSController) SubvolRename(oldPath, newPath string) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	found := false
	for i, fs := range m.Filesystems {
		if fs.Name == oldPath {
			m.Filesystems[i].Name = newPath
			found = true
			break
		}
	}

	var err error
	if !found {
		err = ErrNoFilesystem
	} else if q, ok := m.Quotas[oldPath]; ok {
		delete(m.Quotas, oldPath)
		m.Quotas[newPath] = q
	}

	m.addCallLocked("SubvolRename", err, oldPath, newPath)
	return err
}

func (m *MockBtrFSController) QuotaEnable(path string) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	m.addCallLocked("QuotaEnable", nil, path)
	return nil
}

func (m *MockBtrFSController) QGroupShow(path string) (uint64, error) {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	val := m.Quotas[path]
	m.addCallLocked("QGroupShow", nil, path)
	return val, nil
}

func (m *MockBtrFSController) QGroupLimit(path string, bytes uint64) error {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	if bytes == 0 {
		delete(m.Quotas, path)
	} else {
		m.Quotas[path] = bytes
	}

	m.addCallLocked("QGroupLimit", nil, path, bytes)
	return nil
}
