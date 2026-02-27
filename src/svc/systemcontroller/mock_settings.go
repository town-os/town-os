package systemcontroller

import (
	"context"
	"fmt"
	"maps"
)

// --- Settings ---

func (m *MockClient) GetSettings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetSettings", Args: nil})

	out := make(map[string]string)
	maps.Copy(out, m.Settings)
	return out, nil
}

func (m *MockClient) GetSetting(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetSetting", Args: []any{key}})

	v, ok := m.Settings[key]
	if !ok {
		return "", fmt.Errorf("setting %q not found", key)
	}
	return v, nil
}

func (m *MockClient) SetSetting(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetSetting", Args: []any{key, value}})

	if m.Settings == nil {
		m.Settings = make(map[string]string)
	}
	m.Settings[key] = value
	return nil
}
