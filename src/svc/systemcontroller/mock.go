package systemcontroller

import (
	"fmt"
	"sync"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

type MockClient struct {
	mu              sync.Mutex
	Filesystems     map[string]storage.Filesystem
	Repositories    []RepositoryInfo
	Packages        []string
	Questions       map[string]map[string]packages.Question
	Installed       []string
	StoredResponses map[string]packages.Responses
	Units           []systemd.UnitStatus
	JournalEntries  []systemd.JournalEntry
	Calls           []MockCall
	CreateErr       error
	ModifyErr       error
	RemoveErr       error
	ListErr         error
	AddRepoErr      error
	RemRepoErr      error
	ListRepoErr     error
	ListPkgErr      error
	QuestionsErr    error
	InstallPkgErr   error
	UninstallPkgErr error
	ListInstalledErr error
	GetResponsesErr error
	ListUnitsErr    error
	SetStatusErr    error
	LogReplayErr    error
}

type MockCall struct {
	Method string
	Args   []any
}

func InitMockClient() *MockClient {
	return &MockClient{
		Filesystems:     map[string]storage.Filesystem{},
		StoredResponses: map[string]packages.Responses{},
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

// --- Install ---

func (m *MockClient) InstallPackage(name, version string, responses packages.Responses) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "InstallPackage", Args: []any{name, version, responses}})

	if m.InstallPkgErr != nil {
		return m.InstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	m.Installed = append(m.Installed, key)
	m.StoredResponses[key] = responses
	return nil
}

func (m *MockClient) UninstallPackage(name, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UninstallPackage", Args: []any{name, version}})

	if m.UninstallPkgErr != nil {
		return m.UninstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	for i, p := range m.Installed {
		if p == key {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			delete(m.StoredResponses, key)
			return nil
		}
	}

	return fmt.Errorf("%s: not installed", key)
}

func (m *MockClient) ListInstalled() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListInstalled", Args: nil})

	if m.ListInstalledErr != nil {
		return nil, m.ListInstalledErr
	}

	out := make([]string, len(m.Installed))
	copy(out, m.Installed)
	return out, nil
}

func (m *MockClient) GetResponses(name, version string) (packages.Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetResponses", Args: []any{name, version}})

	if m.GetResponsesErr != nil {
		return nil, m.GetResponsesErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s: not installed", key)
	}

	out := make(packages.Responses, len(resp))
	for k, v := range resp {
		out[k] = v
	}
	return out, nil
}

// --- Systemd ---

func (m *MockClient) ListUnits() ([]systemd.UnitStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListUnits", Args: nil})

	if m.ListUnitsErr != nil {
		return nil, m.ListUnitsErr
	}

	out := make([]systemd.UnitStatus, len(m.Units))
	copy(out, m.Units)
	return out, nil
}

func (m *MockClient) SetUnitStatus(name string, action systemd.StatusAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetUnitStatus", Args: []any{name, action}})

	if m.SetStatusErr != nil {
		return m.SetStatusErr
	}

	switch action {
	case systemd.Start, systemd.Stop, systemd.Restart, systemd.Enable, systemd.Disable:
		return nil
	default:
		return fmt.Errorf("%q: %w", action, systemd.ErrInvalidAction)
	}
}

func (m *MockClient) LogReplay(name string) (<-chan systemd.JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "LogReplay", Args: []any{name}})

	if m.LogReplayErr != nil {
		return nil, m.LogReplayErr
	}

	entries := make([]systemd.JournalEntry, len(m.JournalEntries))
	copy(entries, m.JournalEntries)

	ch := make(chan systemd.JournalEntry)
	go func() {
		defer close(ch)
		for _, e := range entries {
			ch <- e
		}
	}()

	return ch, nil
}
