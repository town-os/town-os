package systemd

import (
	"context"
	"fmt"
	"sync"
)

type MockCall struct {
	Method string
	Args   []any
}

type MockManager struct {
	mu        sync.Mutex
	Units     []UnitStatus
	Entries   []JournalEntry
	Calls     []MockCall
	ListErr   error
	StatusErr error
	LogErr    error
}

func InitMockManager() *MockManager {
	return &MockManager{}
}

func (m *MockManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockManager) ListUnits(ctx context.Context) ([]UnitStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ListUnits", Args: nil})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	out := make([]UnitStatus, len(m.Units))
	copy(out, m.Units)
	return out, nil
}

func (m *MockManager) SetStatus(ctx context.Context, unit string, action StatusAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "SetStatus", Args: []any{unit, action}})

	if m.StatusErr != nil {
		return m.StatusErr
	}

	switch action {
	case Start, Stop, Restart, Enable, Disable:
		return nil
	default:
		return fmt.Errorf("%q: %w", action, ErrInvalidAction)
	}
}

func (m *MockManager) LogReplay(ctx context.Context, unit string) (<-chan JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "LogReplay", Args: []any{unit}})

	if m.LogErr != nil {
		return nil, m.LogErr
	}

	entries := make([]JournalEntry, len(m.Entries))
	copy(entries, m.Entries)

	ch := make(chan JournalEntry)
	go func() {
		defer close(ch)
		for _, e := range entries {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
