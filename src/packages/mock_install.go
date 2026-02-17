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
	mu           sync.Mutex
	Installed    []PackageIdentity
	Calls        []MockInstallCall
	InstallErr   error
	UninstallErr error
	ListErr      error
}

func InitMockInstallManager() *MockInstallManager {
	return &MockInstallManager{}
}

func (m *MockInstallManager) GetCalls() []MockInstallCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockInstallCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockInstallManager) Install(repoName, pkgName, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockInstallCall{Method: "Install", Args: []any{repoName, pkgName, version}})

	if m.InstallErr != nil {
		return m.InstallErr
	}

	for _, p := range m.Installed {
		if p.Name == pkgName && p.Version == version {
			return fmt.Errorf("%s@%s: %w", pkgName, version, ErrAlreadyInstalled)
		}
	}

	m.Installed = append(m.Installed, PackageIdentity{Name: pkgName, Version: version})
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
