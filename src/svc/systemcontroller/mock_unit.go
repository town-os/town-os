package systemcontroller

import (
	"context"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
)

// --- Systemd ---

func (m *MockClient) ListUnits(_ context.Context, params ListParams) (*PageResult[UnitListEntry], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListUnits", Args: []any{params}})

	if m.ListUnitsErr != nil {
		return nil, m.ListUnitsErr
	}

	entries := make([]UnitListEntry, len(m.Units))
	for i, u := range m.Units {
		entries[i] = UnitListEntry{UnitStatus: u}
	}
	entries = filterSearch(entries, params.Search)
	result := paginate(entries, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) SetUnitStatus(_ context.Context, name string, action systemd.StatusAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetUnitStatus", Args: []any{name, action}})

	if m.SetStatusErr != nil {
		return m.SetStatusErr
	}

	switch action {
	case systemd.Start, systemd.Stop, systemd.Restart:
		return nil
	default:
		return fmt.Errorf("%q: %w", action, systemd.ErrInvalidAction)
	}
}

func (m *MockClient) LogReplay(ctx context.Context, name string) (<-chan systemd.JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "LogReplay", Args: []any{name}})

	if m.LogReplayErr != nil {
		return nil, m.LogReplayErr
	}

	entries := make([]systemd.JournalEntry, len(m.JournalEntries))
	copy(entries, m.JournalEntries)

	ch := make(chan systemd.JournalEntry)
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

func (m *MockClient) LogTail(_ context.Context, p systemd.LogTailParams) (systemd.LogTailResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "LogTail", Args: []any{p}})

	if m.LogReplayErr != nil {
		return systemd.LogTailResult{}, m.LogReplayErr
	}

	entries := make([]systemd.JournalEntry, len(m.JournalEntries))
	copy(entries, m.JournalEntries)

	endIdx := len(entries)
	if p.BeforeCursor != "" {
		for i, e := range entries {
			if e.Cursor == p.BeforeCursor {
				endIdx = i
				break
			}
		}
	}

	startIdx := max(endIdx-p.Lines, 0)

	page := entries[startIdx:endIdx]

	var cursor string
	if len(page) > 0 {
		cursor = page[0].Cursor
	}

	return systemd.LogTailResult{Entries: page, Cursor: cursor}, nil
}
