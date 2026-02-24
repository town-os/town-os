package systemd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type MockCall struct {
	Method string
	Args   []any
}

type MockManager struct {
	mu               sync.Mutex
	Units            []UnitStatus
	Entries          []JournalEntry
	Calls            []MockCall
	InstalledUnits   map[string]bool
	ListErr          error
	StatusErr        error
	LogErr           error
	InstallUnitErr   error
	UninstallUnitErr error
	ListPackageUnitFilesErr error
}

func InitMockManager() *MockManager {
	return &MockManager{
		InstalledUnits: make(map[string]bool),
	}
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

func (m *MockManager) LogTail(_ context.Context, p LogTailParams) (LogTailResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "LogTail", Args: []any{p}})

	if m.LogErr != nil {
		return LogTailResult{}, m.LogErr
	}

	entries := make([]JournalEntry, len(m.Entries))
	copy(entries, m.Entries)

	grepLower := strings.ToLower(p.Grep)
	matchesGrep := func(e JournalEntry) bool {
		if p.Grep == "" {
			return true
		}
		return strings.Contains(strings.ToLower(e.Message), grepLower)
	}

	// Timestamp seek mode: entries from 'since' time forward, up to 'until'.
	if !p.Since.IsZero() && p.AfterCursor == "" && p.BeforeCursor == "" {
		page := make([]JournalEntry, 0, p.Lines)
		for _, e := range entries {
			if e.RealtimeTimestamp.Before(p.Since) {
				continue
			}
			if !p.Until.IsZero() && !e.RealtimeTimestamp.Before(p.Until) {
				break
			}
			if !matchesGrep(e) {
				continue
			}
			page = append(page, e)
			if len(page) >= p.Lines {
				break
			}
		}
		var cursor, endCursor string
		if len(page) > 0 {
			cursor = page[0].Cursor
			endCursor = page[len(page)-1].Cursor
		}
		return LogTailResult{Entries: page, Cursor: cursor, EndCursor: endCursor}, nil
	}

	// Forward mode: entries after cursor (grep applied during iteration).
	if p.AfterCursor != "" {
		startIdx := len(entries)
		for i, e := range entries {
			if e.Cursor == p.AfterCursor {
				startIdx = i + 1
				break
			}
		}
		page := make([]JournalEntry, 0, p.Lines)
		for i := startIdx; i < len(entries) && len(page) < p.Lines; i++ {
			if !matchesGrep(entries[i]) {
				continue
			}
			page = append(page, entries[i])
		}
		var cursor, endCursor string
		if len(page) > 0 {
			cursor = page[0].Cursor
			endCursor = page[len(page)-1].Cursor
		}
		return LogTailResult{Entries: page, Cursor: cursor, EndCursor: endCursor}, nil
	}

	// Backward mode: entries before cursor (or from end), grep during iteration.
	endIdx := len(entries)
	if p.BeforeCursor != "" {
		for i, e := range entries {
			if e.Cursor == p.BeforeCursor {
				endIdx = i
				break
			}
		}
	}

	page := make([]JournalEntry, 0, p.Lines)
	for i := endIdx - 1; i >= 0 && len(page) < p.Lines; i-- {
		if !matchesGrep(entries[i]) {
			continue
		}
		page = append(page, entries[i])
	}

	// Reverse to chronological order.
	for i, k := 0, len(page)-1; i < k; i, k = i+1, k-1 {
		page[i], page[k] = page[k], page[i]
	}

	var cursor, endCursor string
	if len(page) > 0 {
		cursor = page[0].Cursor
		endCursor = page[len(page)-1].Cursor
	}

	return LogTailResult{Entries: page, Cursor: cursor, EndCursor: endCursor}, nil
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

	m.InstalledUnits[name] = true

	return nil
}

func (m *MockManager) UninstallUnit(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "UninstallUnit", Args: []any{name}})

	if m.UninstallUnitErr != nil {
		return m.UninstallUnitErr
	}

	delete(m.InstalledUnits, name)

	return nil
}

func (m *MockManager) ListPackageUnitFiles(ctx context.Context, repo, pkgName, version string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ListPackageUnitFiles", Args: []any{repo, pkgName, version}})

	if m.ListPackageUnitFilesErr != nil {
		return nil, m.ListPackageUnitFilesErr
	}

	prefix := fmt.Sprintf("%s%s-%s-%s", PackageUnitPrefix, repo, pkgName, version)
	var names []string
	for name := range m.InstalledUnits {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	return names, nil
}
