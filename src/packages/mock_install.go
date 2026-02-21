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
	mu               sync.Mutex
	Installed        []PackageIdentity
	StoredResponses  map[string]Responses
	Disabled         map[string]bool
	Calls            []MockInstallCall
	InstallErr       error
	UninstallErr     error
	ListErr          error
	GetResponsesErr  error
	SetDisabledErr   error
	IsDisabledErr    error
}

func InitMockInstallManager() *MockInstallManager {
	return &MockInstallManager{
		StoredResponses: map[string]Responses{},
		Disabled:        map[string]bool{},
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
		if p.Name == pkgName && p.Version == version {
			return fmt.Errorf("%s@%s: %w", pkgName, version, ErrAlreadyInstalled)
		}
	}

	m.Installed = append(m.Installed, PackageIdentity{Name: pkgName, Version: version})
	key := fmt.Sprintf("%s@%s", pkgName, version)
	m.StoredResponses[key] = responses
	return nil
}

func (m *MockInstallManager) Uninstall(pkgName, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "Uninstall", Args: []any{pkgName, version}})

	if m.UninstallErr != nil {
		return m.UninstallErr
	}

	for i, p := range m.Installed {
		if p.Name == pkgName && p.Version == version {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			key := fmt.Sprintf("%s@%s", pkgName, version)
			delete(m.StoredResponses, key)
			return nil
		}
	}

	return fmt.Errorf("%s@%s: %w", pkgName, version, ErrNotInstalled)
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

func (m *MockInstallManager) GetResponses(pkgName, version string) (Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "GetResponses", Args: []any{pkgName, version}})

	if m.GetResponsesErr != nil {
		return nil, m.GetResponsesErr
	}

	key := fmt.Sprintf("%s@%s", pkgName, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s@%s: %w", pkgName, version, ErrNotInstalled)
	}

	out := make(Responses, len(resp))
	for k, v := range resp {
		out[k] = v
	}
	return out, nil
}

func (m *MockInstallManager) SetDisabled(pkgName string, disabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "SetDisabled", Args: []any{pkgName, disabled}})

	if m.SetDisabledErr != nil {
		return m.SetDisabledErr
	}

	if disabled {
		m.Disabled[pkgName] = true
	} else {
		delete(m.Disabled, pkgName)
	}
	return nil
}

func (m *MockInstallManager) IsDisabled(pkgName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "IsDisabled", Args: []any{pkgName}})

	if m.IsDisabledErr != nil {
		return false, m.IsDisabledErr
	}

	return m.Disabled[pkgName], nil
}
