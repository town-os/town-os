package storage

import (
	"fmt"
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

	// Real btrfs refuses to delete a subvolume that contains child subvolumes.
	childPrefix := name + "/"
	for _, fs := range m.Filesystems {
		if strings.HasPrefix(fs.Name, childPrefix) {
			err := fmt.Errorf("btrfs subvolume delete: directory not empty: %s", name)
			m.addCallLocked("SubvolDelete", err, name)
			return err
		}
	}

	m.removeFilesystemLocked(name)
	delete(m.Quotas, name)

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

	// Create a copy of the source filesystem at the destination path.
	m.addFilesystemLocked(dst)
	if q, ok := m.Quotas[src]; ok {
		m.Quotas[dst] = q
	}

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

	if !found {
		err := ErrNoFilesystem
		m.addCallLocked("SubvolRename", err, oldPath, newPath)
		return err
	}

	// Rename quota for the exact match.
	if q, ok := m.Quotas[oldPath]; ok {
		delete(m.Quotas, oldPath)
		m.Quotas[newPath] = q
	}

	// Also rename children (os.Rename on a real directory moves all contents).
	childPrefix := oldPath + "/"
	for i, fs := range m.Filesystems {
		if after, ok := strings.CutPrefix(fs.Name, childPrefix); ok {
			newChildName := fmt.Sprintf("%s/%s", newPath, after)
			m.Filesystems[i].Name = newChildName
			if q, ok := m.Quotas[fs.Name]; ok {
				delete(m.Quotas, fs.Name)
				m.Quotas[newChildName] = q
			}
		}
	}

	m.addCallLocked("SubvolRename", nil, oldPath, newPath)
	return nil
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
