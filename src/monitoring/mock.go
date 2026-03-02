package monitoring

import (
	"context"
	"sync"
)

// MockRunner is a thread-safe mock implementation of Runner for unit testing.
// It records all method calls and supports injectable errors and return values.
type MockRunner struct {
	mu       sync.Mutex
	Calls    []MockCall
	Running  map[string]bool
	RunErr   error
	StopErr  error
	CheckErr error
}

// MockCall records a single method invocation on MockRunner.
type MockCall struct {
	Method string
	Args   []any
}

// InitMockRunner creates a MockRunner with empty state, ready for testing.
func InitMockRunner() *MockRunner {
	return &MockRunner{
		Running: map[string]bool{},
	}
}

// Run records the call and marks the container as running (unless RunErr is set).
//
// Parameters:
//   - ctx: context for cancellation.
//   - args: full argument list for the container run command.
func (m *MockRunner) Run(_ context.Context, args []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "Run", Args: []any{args}})

	if m.RunErr != nil {
		return m.RunErr
	}

	// Extract container name from args (--name flag).
	for i, arg := range args {
		if arg == "--name" && i+1 < len(args) {
			m.Running[args[i+1]] = true
			break
		}
	}

	return nil
}

// Stop records the call and marks the container as not running.
//
// Parameters:
//   - ctx: context for cancellation.
//   - name: the container name to stop.
func (m *MockRunner) Stop(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "Stop", Args: []any{name}})

	if m.StopErr != nil {
		return m.StopErr
	}

	delete(m.Running, name)
	return nil
}

// IsRunning records the call and returns the mock running state for the container.
//
// Parameters:
//   - ctx: context for cancellation.
//   - name: the container name to check.
func (m *MockRunner) IsRunning(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "IsRunning", Args: []any{name}})

	if m.CheckErr != nil {
		return false, m.CheckErr
	}

	return m.Running[name], nil
}

// GetCalls returns a snapshot of all recorded method calls.
func (m *MockRunner) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}
