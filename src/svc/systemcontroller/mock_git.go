package systemcontroller

import "context"

func (m *MockClient) RebuildGitSources(_ context.Context, repo, name, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RebuildGitSources", Args: []any{repo, name, version}})

	if m.RebuildGitErr != nil {
		return m.RebuildGitErr
	}
	return nil
}
