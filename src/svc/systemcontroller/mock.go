package systemcontroller

import (
	"fmt"
	"sync"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

type MockClient struct {
	mu           sync.Mutex
	Filesystems  map[string]storage.Filesystem
	Repositories []RepositoryInfo
	Packages     []string
	Questions    map[string]map[string]packages.Question
	Calls        []MockCall
	CreateErr    error
	ModifyErr    error
	RemoveErr    error
	ListErr      error
	AddRepoErr   error
	RemRepoErr   error
	ListRepoErr  error
	ListPkgErr   error
	QuestionsErr error
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

// --- Storage ---

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

// --- Repository ---

func (m *MockClient) AddRepository(rawURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddRepository", Args: []any{rawURL}})

	if m.AddRepoErr != nil {
		return m.AddRepoErr
	}

	for _, r := range m.Repositories {
		if r.URL == rawURL {
			return fmt.Errorf("repository %s already exists", rawURL)
		}
	}

	m.Repositories = append(m.Repositories, RepositoryInfo{Name: rawURL, URL: rawURL})
	return nil
}

func (m *MockClient) RemoveRepository(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveRepository", Args: []any{name}})

	if m.RemRepoErr != nil {
		return m.RemRepoErr
	}

	for i, r := range m.Repositories {
		if r.Name == name {
			m.Repositories = append(m.Repositories[:i], m.Repositories[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("repository %s not found", name)
}

func (m *MockClient) ListRepositories() ([]RepositoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListRepositories", Args: nil})

	if m.ListRepoErr != nil {
		return nil, m.ListRepoErr
	}

	out := make([]RepositoryInfo, len(m.Repositories))
	copy(out, m.Repositories)
	return out, nil
}

// --- Packages ---

func (m *MockClient) ListPackages() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackages", Args: nil})

	if m.ListPkgErr != nil {
		return nil, m.ListPkgErr
	}

	out := make([]string, len(m.Packages))
	copy(out, m.Packages)
	return out, nil
}

func (m *MockClient) GetPackageQuestions(name string) (map[string]packages.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetPackageQuestions", Args: []any{name}})

	if m.QuestionsErr != nil {
		return nil, m.QuestionsErr
	}

	questions, ok := m.Questions[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	out := make(map[string]packages.Question, len(questions))
	for k, v := range questions {
		out[k] = v
	}
	return out, nil
}
