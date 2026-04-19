package systemd

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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
	InstalledUnits   map[string]string // name → content
	ListErr          error
	StatusErr        error
	LogErr           error
	InstallUnitErr   error
	UninstallUnitErr error
	ListPackageUnitFilesErr error
}

func InitMockManager() *MockManager {
	return &MockManager{
		InstalledUnits: make(map[string]string),
	}
}

func (m *MockManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockManager) ClearCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
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

func (m *MockManager) GetUnitStates(_ context.Context, names []string) ([]UnitStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	argsCopy := make([]string, len(names))
	copy(argsCopy, names)
	m.Calls = append(m.Calls, MockCall{Method: "GetUnitStates", Args: []any{argsCopy}})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	byName := make(map[string]UnitStatus, len(m.Units))
	for _, u := range m.Units {
		byName[u.Name] = u
	}

	out := make([]UnitStatus, len(names))
	for i, n := range names {
		if u, ok := byName[n]; ok {
			out[i] = u
			continue
		}
		out[i] = UnitStatus{Name: n, LoadState: "not-found", ActiveState: "inactive"}
	}
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
	matchesFilter := func(e JournalEntry) bool {
		if p.Grep != "" && !strings.Contains(strings.ToLower(e.Message), grepLower) {
			return false
		}
		if p.Priority > 0 && e.Priority != "" {
			pri, err := strconv.Atoi(e.Priority)
			if err == nil && pri > p.Priority {
				return false
			}
		}
		return true
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
			if !matchesFilter(e) {
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
			if !matchesFilter(entries[i]) {
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
		if !matchesFilter(entries[i]) {
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

func (m *MockManager) LogReplay(ctx context.Context, units ...string) (<-chan JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Normalize the recorded args so tests can assert a single-unit call
	// as `Args[0] == "foo.service"` while multi-unit calls inspect the
	// slice. Mirrors how SetStatus records its unit name.
	argsCopy := append([]string(nil), units...)
	var arg any
	switch len(argsCopy) {
	case 0:
		arg = ""
	case 1:
		arg = argsCopy[0]
	default:
		arg = argsCopy
	}
	m.Calls = append(m.Calls, MockCall{Method: "LogReplay", Args: []any{arg}})

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

	m.InstalledUnits[name] = content

	// Mirror real systemd: a freshly-installed unit is visible to
	// `list-units --all` as loaded/inactive immediately, and
	// SetStatus(Start) flips it to active. Tests that go through the
	// install handler and then query /systemd/units-tree rely on this:
	// without it, ListUnits returns [] and the tree looks empty.
	for _, u := range m.Units {
		if u.Name == name {
			return nil
		}
	}
	m.Units = append(m.Units, UnitStatus{
		Name:        name,
		LoadState:   "loaded",
		ActiveState: "inactive",
		SubState:    "dead",
	})

	return nil
}

func (m *MockManager) ReadUnit(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.InstalledUnits[name]
	if !ok {
		return "", ErrUnitNotFound
	}
	return content, nil
}

func (m *MockManager) UninstallUnit(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "UninstallUnit", Args: []any{name}})

	if m.UninstallUnitErr != nil {
		return m.UninstallUnitErr
	}

	delete(m.InstalledUnits, name)
	// Keep m.Units in sync with the install state so a test that uninstalls
	// a package sees ListUnits drop the entry, mirroring real systemd.
	for i, u := range m.Units {
		if u.Name == name {
			m.Units = append(m.Units[:i], m.Units[i+1:]...)
			break
		}
	}

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
