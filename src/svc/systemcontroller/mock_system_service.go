package systemcontroller

import (
	"context"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
)

// ListSystemServices returns the mock system services list.
func (m *MockClient) ListSystemServices(_ context.Context) ([]SystemServiceEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ListSystemServices"})

	if m.ListSystemServicesErr != nil {
		return nil, m.ListSystemServicesErr
	}

	out := make([]SystemServiceEntry, len(m.SystemServices))
	copy(out, m.SystemServices)
	return out, nil
}

// SetSystemServiceStatus applies an action to a mock system service.
func (m *MockClient) SetSystemServiceStatus(_ context.Context, key string, action systemd.StatusAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "SetSystemServiceStatus", Args: []any{key, action}})

	if m.SetSystemServiceStatusErr != nil {
		return m.SetSystemServiceStatusErr
	}

	if action == systemd.Enable || action == systemd.Disable {
		return fmt.Errorf("action %q is not allowed for system services", action)
	}

	return nil
}
