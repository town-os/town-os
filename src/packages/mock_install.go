package packages

import (
	"fmt"
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
}

func InitMockInstallManager() *MockInstallManager {
	return &MockInstallManager{
		StoredResponses: map[string]Responses{},
		LastResponses:   map[string]Responses{},
		Children:        map[string][]string{},
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

func (m *MockInstallManager) Install(repoName, pkgName, version string, responses Responses) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "Install", Args: []any{repoName, pkgName, version, responses}})

	if m.InstallErr != nil {
		return m.InstallErr
	}

	for _, p := range m.Installed {
		if p.Repo == repoName && p.Name == pkgName && p.Version == version {
			return fmt.Errorf("%s/%s@%s: %w", repoName, pkgName, version, ErrAlreadyInstalled)
		}
	}

	m.Installed = append(m.Installed, PackageIdentity{Repo: repoName, Name: pkgName, Version: version})
	key := fmt.Sprintf("%s/%s@%s", repoName, pkgName, version)
	m.StoredResponses[key] = responses
	return nil
}

func (m *MockInstallManager) Uninstall(repoName, pkgName, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "Uninstall", Args: []any{repoName, pkgName, version}})

	if m.UninstallErr != nil {
		return m.UninstallErr
	}

	for i, p := range m.Installed {
		if p.Repo == repoName && p.Name == pkgName && p.Version == version {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			key := fmt.Sprintf("%s/%s@%s", repoName, pkgName, version)
			delete(m.StoredResponses, key)
			return nil
		}
	}

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
	for k, v := range resp {
		out[k] = v
	}
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
