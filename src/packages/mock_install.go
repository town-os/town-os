package packages

import (
	"fmt"
	"maps"
	"sync"
)

type MockInstallCall struct {
	Method string
	Args   []any
}

type MockInstallManager struct {
	mu                  sync.Mutex
	Installed           []PackageIdentity
	StoredResponses     map[string]Responses
	LastResponses       map[string]Responses
	Children            map[string][]string
	Dependencies        map[string]map[string]DependencyRecord
	Networks            map[string]string
	Disabled            map[string]bool
	ChangedPackages     map[string]bool
	Calls               []MockInstallCall
	InstallErr          error
	UninstallErr        error
	ListErr             error
	GetResponsesErr     error
	SetDisabledErr      error
	IsDisabledErr       error
	IsPackageChangedErr error

	// OnInstall is called during Install while the mock lock is NOT held,
	// allowing tests to inject delays or synchronization points.
	OnInstall func()
	// OnUninstall is called during Uninstall while the mock lock is NOT held.
	OnUninstall func()
}

func InitMockInstallManager() *MockInstallManager {
	return &MockInstallManager{
		StoredResponses: map[string]Responses{},
		LastResponses:   map[string]Responses{},
		Children:        map[string][]string{},
		Dependencies:    map[string]map[string]DependencyRecord{},
		Networks:        map[string]string{},
		Disabled:        map[string]bool{},
		ChangedPackages: map[string]bool{},
	}
}

func (m *MockInstallManager) GetCalls() []MockInstallCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockInstallCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockInstallManager) Install(repoName, pkgName, sourcePkgName, version string, responses Responses) error {
	m.mu.Lock()
	hook := m.OnInstall
	m.Calls = append(m.Calls, MockInstallCall{Method: "Install", Args: []any{repoName, pkgName, sourcePkgName, version, responses}})

	if m.InstallErr != nil {
		m.mu.Unlock()
		return m.InstallErr
	}

	for _, p := range m.Installed {
		if p.Repo == repoName && p.Name == pkgName && p.Version == version {
			m.mu.Unlock()
			return fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrAlreadyInstalled)
		}
	}

	m.Installed = append(m.Installed, PackageIdentity{Repo: repoName, Name: pkgName, Version: version})
	key := fmt.Sprintf("%s/%s@%s", repoName, pkgName, version)
	m.StoredResponses[key] = responses
	m.mu.Unlock()

	if hook != nil {
		hook()
	}

	return nil
}

func (m *MockInstallManager) Uninstall(repoName, pkgName, version string) error {
	m.mu.Lock()
	hook := m.OnUninstall
	m.Calls = append(m.Calls, MockInstallCall{Method: "Uninstall", Args: []any{repoName, pkgName, version}})

	if m.UninstallErr != nil {
		m.mu.Unlock()
		return m.UninstallErr
	}

	for i, p := range m.Installed {
		if p.Repo == repoName && p.Name == pkgName && p.Version == version {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			key := fmt.Sprintf("%s/%s@%s", repoName, pkgName, version)
			delete(m.StoredResponses, key)
			m.mu.Unlock()
			if hook != nil {
				hook()
			}
			return nil
		}
	}

	m.mu.Unlock()
	return fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrNotInstalled)
}

func (m *MockInstallManager) ListInstalled() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "ListInstalled", Args: nil})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	out := make([]string, len(m.Installed))
	for i, p := range m.Installed {
		out[i] = p.String()
	}
	return out, nil
}

func (m *MockInstallManager) GetInstalledVersion(repoName, pkgName string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "GetInstalledVersion", Args: []any{repoName, pkgName}})

	for _, p := range m.Installed {
		if p.Repo == repoName && p.Name == pkgName {
			return p.Version, true, nil
		}
	}
	return "", false, nil
}

func (m *MockInstallManager) GetResponses(repoName, pkgName, version string) (Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "GetResponses", Args: []any{repoName, pkgName, version}})

	if m.GetResponsesErr != nil {
		return nil, m.GetResponsesErr
	}

	key := fmt.Sprintf("%s/%s@%s", repoName, pkgName, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrNotInstalled)
	}

	out := make(Responses, len(resp))
	maps.Copy(out, resp)
	return out, nil
}

func (m *MockInstallManager) SetDisabled(repoName, pkgName string, disabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "SetDisabled", Args: []any{repoName, pkgName, disabled}})

	if m.SetDisabledErr != nil {
		return m.SetDisabledErr
	}

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	if disabled {
		m.Disabled[key] = true
	} else {
		delete(m.Disabled, key)
	}
	return nil
}

func (m *MockInstallManager) IsDisabled(repoName, pkgName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "IsDisabled", Args: []any{repoName, pkgName}})

	if m.IsDisabledErr != nil {
		return false, m.IsDisabledErr
	}

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	return m.Disabled[key], nil
}

func (m *MockInstallManager) SaveLastResponses(repoName, pkgName string, responses Responses) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "SaveLastResponses", Args: []any{repoName, pkgName, responses}})

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	m.LastResponses[key] = responses
	return nil
}

func (m *MockInstallManager) LoadLastResponses(repoName, pkgName string) (Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "LoadLastResponses", Args: []any{repoName, pkgName}})

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	resp, ok := m.LastResponses[key]
	if !ok {
		return nil, fmt.Errorf("no last responses for %s", key)
	}
	return resp, nil
}

func (m *MockInstallManager) ClearLastResponses(repoName, pkgName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "ClearLastResponses", Args: []any{repoName, pkgName}})

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	delete(m.LastResponses, key)
	return nil
}

func (m *MockInstallManager) SaveChildren(repoName, parentName string, children []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "SaveChildren", Args: []any{repoName, parentName, children}})

	key := fmt.Sprintf("%s/%s", repoName, parentName)
	m.Children[key] = children
	return nil
}

func (m *MockInstallManager) LoadChildren(repoName, parentName string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "LoadChildren", Args: []any{repoName, parentName}})

	key := fmt.Sprintf("%s/%s", repoName, parentName)
	children, ok := m.Children[key]
	if !ok {
		return nil, nil
	}
	return children, nil
}

func (m *MockInstallManager) IsPackageChanged(repoName, pkgName, version string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "IsPackageChanged", Args: []any{repoName, pkgName, version}})

	if m.IsPackageChangedErr != nil {
		return false, m.IsPackageChangedErr
	}

	key := fmt.Sprintf("%s/%s@%s", repoName, pkgName, version)
	return m.ChangedPackages[key], nil
}

func (m *MockInstallManager) SaveDependencies(repoName, pkgName string, deps map[string]DependencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "SaveDependencies", Args: []any{repoName, pkgName, deps}})

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	m.Dependencies[key] = deps
	return nil
}

func (m *MockInstallManager) LoadDependencies(repoName, pkgName string) (map[string]DependencyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "LoadDependencies", Args: []any{repoName, pkgName}})

	key := fmt.Sprintf("%s/%s", repoName, pkgName)
	deps, ok := m.Dependencies[key]
	if !ok {
		return nil, nil //nolint:nilnil // nil deps is the correct zero value when no dependencies are stored
	}
	return deps, nil
}

func (m *MockInstallManager) SaveNetwork(repoName, pkgName, network string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "SaveNetwork", Args: []any{repoName, pkgName, network}})

	m.Networks[fmt.Sprintf("%s/%s", repoName, pkgName)] = network
	return nil
}

func (m *MockInstallManager) LoadNetwork(repoName, pkgName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "LoadNetwork", Args: []any{repoName, pkgName}})

	return m.Networks[fmt.Sprintf("%s/%s", repoName, pkgName)], nil
}
