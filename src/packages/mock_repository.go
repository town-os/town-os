package packages

import (
	"fmt"
	"sync"
)

type MockRepositoryCall struct {
	Method string
	Args   []any
}

type MockRepositoryManager struct {
	mu              sync.Mutex
	Items           []Repository
	Packages        PackageTable
	Calls           []MockRepositoryCall
	AddErr          error
	RemoveErr       error
	ListReposErr    error
	RefreshErr      error
	LoadAllErr      error
	ListPackagesErr error
	LatestErr       error
	QuestionsErr    error
	FindRepoErr     error
}

func InitMockRepositoryManager() *MockRepositoryManager {
	return &MockRepositoryManager{
		Packages: PackageTable{},
	}
}

func (m *MockRepositoryManager) GetCalls() []MockRepositoryCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockRepositoryCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockRepositoryManager) Add(repo Repository) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "Add", Args: []any{repo}})

	if m.AddErr != nil {
		return m.AddErr
	}

	for _, r := range m.Items {
		if r.Name == repo.Name {
			return fmt.Errorf("repository %s already exists", repo.Name)
		}
	}

	m.Items = append(m.Items, repo)
	return nil
}

func (m *MockRepositoryManager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "Remove", Args: []any{name}})

	if m.RemoveErr != nil {
		return m.RemoveErr
	}

	for i, r := range m.Items {
		if r.Name == name {
			m.Items = append(m.Items[:i], m.Items[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("repository %s not found", name)
}

func (m *MockRepositoryManager) Get(name string) (Repository, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "Get", Args: []any{name}})

	for _, r := range m.Items {
		if r.Name == name {
			return r, true
		}
	}

	return Repository{}, false
}

func (m *MockRepositoryManager) List() ([]Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "List", Args: nil})

	if m.ListReposErr != nil {
		return nil, m.ListReposErr
	}

	out := make([]Repository, len(m.Items))
	copy(out, m.Items)
	return out, nil
}

func (m *MockRepositoryManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "Refresh", Args: nil})

	if m.RefreshErr != nil {
		return m.RefreshErr
	}

	return nil
}

func (m *MockRepositoryManager) LoadAllPackages() (PackageTable, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "LoadAllPackages", Args: nil})

	if m.LoadAllErr != nil {
		return nil, m.LoadAllErr
	}

	out := PackageTable{}
	for name, versions := range m.Packages {
		out[name] = map[string]InputPackage{}
		for version, pkg := range versions {
			out[name][version] = pkg
		}
	}

	return out, nil
}

func (m *MockRepositoryManager) ListPackages() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "ListPackages", Args: nil})

	if m.ListPackagesErr != nil {
		return nil, m.ListPackagesErr
	}

	best := map[string]string{}
	for name, versions := range m.Packages {
		for version := range versions {
			prev, exists := best[name]
			if !exists || CompareVersions(version, prev) > 0 {
				best[name] = version
			}
		}
	}

	out := make([]string, 0, len(best))
	for name, version := range best {
		out = append(out, PackageIdentity{Name: name, Version: version}.String())
	}

	return out, nil
}

func (m *MockRepositoryManager) LatestPackage(name string) (InputPackage, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "LatestPackage", Args: []any{name}})

	if m.LatestErr != nil {
		return InputPackage{}, "", m.LatestErr
	}

	versions, ok := m.Packages[name]
	if !ok {
		return InputPackage{}, "", ErrPackageNotFound
	}

	var bestPkg InputPackage
	var bestVersion string
	found := false

	for version, pkg := range versions {
		if !found || CompareVersions(version, bestVersion) > 0 {
			bestVersion = version
			bestPkg = pkg
			found = true
		}
	}

	if !found {
		return InputPackage{}, "", ErrPackageNotFound
	}

	return bestPkg, bestVersion, nil
}

func (m *MockRepositoryManager) GetPackageQuestions(name string) (map[string]Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "GetPackageQuestions", Args: []any{name}})

	if m.QuestionsErr != nil {
		return nil, m.QuestionsErr
	}

	versions, ok := m.Packages[name]
	if !ok {
		return nil, ErrPackageNotFound
	}

	var bestPkg InputPackage
	var bestVersion string
	found := false

	for version, pkg := range versions {
		if !found || CompareVersions(version, bestVersion) > 0 {
			bestVersion = version
			bestPkg = pkg
			found = true
		}
	}

	if !found {
		return nil, ErrPackageNotFound
	}

	return bestPkg.Questions, nil
}

func (m *MockRepositoryManager) FindRepoForPackage(name, version string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "FindRepoForPackage", Args: []any{name, version}})

	if m.FindRepoErr != nil {
		return "", m.FindRepoErr
	}

	versions, ok := m.Packages[name]
	if !ok {
		return "", ErrPackageNotFound
	}

	if _, ok := versions[version]; !ok {
		return "", ErrPackageNotFound
	}

	return "mock-repo", nil
}
