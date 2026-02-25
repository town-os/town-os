package packages

import (
	"fmt"
	"sort"
	"sync"
)

type MockRepositoryCall struct {
	Method string
	Args   []any
}

type MockRepositoryManager struct {
	mu                     sync.Mutex
	Items                  []Repository
	Packages               PackageTable
	FeaturedMap            map[string][]string
	Calls                  []MockRepositoryCall
	AddErr                 error
	RemoveErr              error
	ListReposErr           error
	LoadAllErr             error
	ListPackagesErr        error
	ListPackageVersionsErr error
	LatestErr              error
	QuestionsErr           error
	FindRepoErr            error
	MoveErr                error
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

func (m *MockRepositoryManager) Move(name string, position int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "Move", Args: []any{name, position}})

	if m.MoveErr != nil {
		return m.MoveErr
	}

	idx := -1
	for i, r := range m.Items {
		if r.Name == name {
			idx = i
			break
		}
	}

	if idx == -1 {
		return fmt.Errorf("repository %s not found", name)
	}

	repo := m.Items[idx]
	m.Items = append(m.Items[:idx], m.Items[idx+1:]...)

	if position < 0 {
		position = 0
	}
	if position > len(m.Items) {
		position = len(m.Items)
	}

	m.Items = append(m.Items[:position], append([]Repository{repo}, m.Items[position:]...)...)

	return nil
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

func (m *MockRepositoryManager) Refresh() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "Refresh", Args: nil})
}

func (m *MockRepositoryManager) ForceRefresh() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "ForceRefresh", Args: nil})
}

func (m *MockRepositoryManager) RefreshErrors() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *MockRepositoryManager) ListPackagesByRepo() ([]RepoPackageGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "ListPackagesByRepo", Args: nil})

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

	pkgs := make([]PackageIdentity, 0, len(best))
	for name, version := range best {
		pkgs = append(pkgs, PackageIdentity{Repo: "mock-repo", Name: name, Version: version})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })

	var featured []string
	if m.FeaturedMap != nil {
		featured = m.FeaturedMap["mock-repo"]
	}
	return []RepoPackageGroup{{Repo: "mock-repo", Packages: pkgs, Featured: featured}}, nil
}

func (m *MockRepositoryManager) ListPackageVersions(name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockRepositoryCall{Method: "ListPackageVersions", Args: []any{name}})

	if m.ListPackageVersionsErr != nil {
		return nil, m.ListPackageVersionsErr
	}

	versions, ok := m.Packages[name]
	if !ok {
		return nil, nil
	}

	out := make([]string, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}

	sort.Slice(out, func(i, j int) bool {
		return CompareVersions(out[i], out[j]) > 0
	})

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
