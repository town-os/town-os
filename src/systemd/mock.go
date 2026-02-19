package systemd

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type MockCall struct {
	Method string
	Args   []any
}

type MockManager struct {
	mu              sync.Mutex
	Units           []UnitStatus
	Entries         []JournalEntry
	Calls           []MockCall
	ListErr         error
	StatusErr       error
	LogErr          error
	InstallUnitErr  error
	UninstallUnitErr error
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

func (m *MockManager) LogTail(_ context.Context, unit string, lines int, beforeCursor string, grep string) (LogTailResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "LogTail", Args: []any{unit, lines, beforeCursor, grep}})

	if m.LogErr != nil {
		return LogTailResult{}, m.LogErr
	}

	entries := make([]JournalEntry, len(m.Entries))
	copy(entries, m.Entries)

	// Filter by grep if set.
	if grep != "" {
		grepLower := strings.ToLower(grep)
		filtered := make([]JournalEntry, 0, len(entries))
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Message), grepLower) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// If beforeCursor is set, find the entry and take entries before it.
	endIdx := len(entries)
	if beforeCursor != "" {
		for i, e := range entries {
			if e.Cursor == beforeCursor {
				endIdx = i
				break
			}
		}
	}

	startIdx := endIdx - lines
	if startIdx < 0 {
		startIdx = 0
	}

	page := entries[startIdx:endIdx]

	var cursor string
	if len(page) > 0 {
		cursor = page[0].Cursor
	}

	return LogTailResult{Entries: page, Cursor: cursor}, nil
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

func (m *MockManager) InstallUnit(ctx context.Context, name string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "InstallUnit", Args: []any{name, content}})

	if m.InstallUnitErr != nil {
		return m.InstallUnitErr
	}

	return nil
}

func (m *MockManager) UninstallUnit(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "UninstallUnit", Args: []any{name}})

	if m.UninstallUnitErr != nil {
		return m.UninstallUnitErr
	}

	return nil
}
