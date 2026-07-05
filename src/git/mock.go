package git

import (
	"context"
	"sync"
)

// MockCall records a single method invocation on MockClient.
type MockCall struct {
	Method string
	Args   []any
}

// MockClient implements Client for testing. It tracks calls and supports
// injectable errors following the project mock pattern.
type MockClient struct {
	mu            sync.Mutex
	Calls         []MockCall
	CloneErr      error
	PullErr       error
	DiffErr       error
	DiffDirty     bool
	StashErr      error
	StashApplyErr error
	FetchErr      error
	CheckoutErr   error
	InitErr       error
	AddErr        error
	CommitErr     error
	RevParseErr   error
	RevParseOut   string
	RunErr        error
	RunOut        []byte
}

func InitMockClient() *MockClient {
	return &MockClient{}
}

// GetCalls returns a copy of the recorded calls.
func (m *MockClient) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockClient) record(method string, args ...any) {
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

func (m *MockClient) Clone(_ context.Context, dir, url, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Clone", dir, url, name)
	return m.CloneErr
}

func (m *MockClient) CloneBranch(_ context.Context, dir, url, name, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("CloneBranch", dir, url, name, branch)
	return m.CloneErr
}

func (m *MockClient) Pull(_ context.Context, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Pull", dir)
	return m.PullErr
}

func (m *MockClient) Diff(_ context.Context, dir string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Diff", dir)
	return m.DiffDirty, m.DiffErr
}

func (m *MockClient) Stash(_ context.Context, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Stash", dir)
	return m.StashErr
}

func (m *MockClient) StashApply(_ context.Context, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("StashApply", dir)
	return m.StashApplyErr
}

func (m *MockClient) Fetch(_ context.Context, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Fetch", dir)
	return m.FetchErr
}

func (m *MockClient) Checkout(_ context.Context, dir, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Checkout", dir, ref)
	return m.CheckoutErr
}

func (m *MockClient) Init(_ context.Context, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Init", dir)
	return m.InitErr
}

func (m *MockClient) Add(_ context.Context, dir string, paths ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Add", dir, paths)
	return m.AddErr
}

func (m *MockClient) Commit(_ context.Context, dir, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Commit", dir, message)
	return m.CommitErr
}

func (m *MockClient) RevParse(_ context.Context, dir, ref string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("RevParse", dir, ref)
	return m.RevParseOut, m.RevParseErr
}

func (m *MockClient) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Run", dir, args)
	return m.RunOut, m.RunErr
}
