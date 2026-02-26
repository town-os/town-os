package packages

import "sync"

// MockGitCall records the arguments passed to a MockGitCloner method.
type MockGitCall struct {
	Method string
	Args   []any
}

// MockGitCloner is a test double for GitCloner that records calls and can
// inject errors.
type MockGitCloner struct {
	mu        sync.Mutex
	Calls     []MockGitCall
	CloneErr  error
	UpdateErr error
}

func (m *MockGitCloner) Clone(targetDir, repoURL, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockGitCall{Method: "Clone", Args: []any{targetDir, repoURL, branch}})

	if m.CloneErr != nil {
		return m.CloneErr
	}
	return nil
}

func (m *MockGitCloner) Update(targetDir, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockGitCall{Method: "Update", Args: []any{targetDir, branch}})

	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	return nil
}

func (m *MockGitCloner) GetCalls() []MockGitCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockGitCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}
