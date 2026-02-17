package systemcontroller

import (
	"fmt"
	"sync"

	"gitea.com/town-os/town-os/src/storage"
)

type MockClient struct {
	mu          sync.Mutex
	Filesystems map[string]storage.Filesystem
	Calls       []MockCall
	CreateErr   error
	ModifyErr   error
	RemoveErr   error
	ListErr     error
}

type MockCall struct {
	Method string
	Args   []any
}

func InitMockClient() *MockClient {
	return &MockClient{
		Filesystems: map[string]storage.Filesystem{},
	}
}

func (m *MockClient) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockClient) CreateFilesystem(fs storage.Filesystem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "CreateFilesystem", Args: []any{fs}})

	if m.CreateErr != nil {
		return m.CreateErr
	}

	m.Filesystems[fs.Name] = fs
	return nil
}

func (m *MockClient) ModifyFilesystem(name string, fs storage.Filesystem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ModifyFilesystem", Args: []any{name, fs}})

	if m.ModifyErr != nil {
		return m.ModifyErr
	}

	if _, ok := m.Filesystems[name]; !ok {
		return fmt.Errorf("filesystem %s not found", name)
	}

	m.Filesystems[name] = fs
	return nil
}

func (m *MockClient) RemoveFilesystem(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveFilesystem", Args: []any{name}})

	if m.RemoveErr != nil {
		return m.RemoveErr
	}

	delete(m.Filesystems, name)
	return nil
}

func (m *MockClient) ListFilesystems(prefix string) ([]storage.Filesystem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListFilesystems", Args: []any{prefix}})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	var out []storage.Filesystem
	for _, fs := range m.Filesystems {
		if prefix == "" || len(fs.Name) >= len(prefix) && fs.Name[:len(prefix)] == prefix {
			out = append(out, fs)
		}
	}

	return out, nil
}
