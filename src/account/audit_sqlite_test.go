package account

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// --- Interface conformance ---

func TestSQLiteAuditManagerImplementsAuditManager(t *testing.T) {
	var _ AuditManager = (*SQLiteAuditManager)(nil)
}

// --- Helpers ---

func initTestAuditDB(t *testing.T) *SQLiteAuditManager {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitAuditManager(db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}
	return mgr
}

// --- LogEntry + List lifecycle ---

func TestAuditLogEntryAndList(t *testing.T) {
	mgr := initTestAuditDB(t)

	entry := AuditEntry{
		Account:   "alice",
		Action:    "create filesystem",
		Path:      "/storage/create",
		Success:   true,
		CreatedAt: time.Now().UTC(),
	}

	if err := mgr.LogEntry(entry); err != nil {
		t.Fatalf("LogEntry: %v", err)
	}

	page, err := mgr.List(AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}

	got := page.Entries[0]
	if got.Account != "alice" {
		t.Fatalf("expected account %q, got %q", "alice", got.Account)
	}
	if got.Action != "create filesystem" {
		t.Fatalf("expected action %q, got %q", "create filesystem", got.Action)
	}
	if got.Path != "/storage/create" {
		t.Fatalf("expected path %q, got %q", "/storage/create", got.Path)
	}
	if !got.Success {
		t.Fatal("expected success to be true")
	}
	if got.Error != "" {
		t.Fatalf("expected empty error, got %q", got.Error)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
}

func TestAuditLogEntryWithError(t *testing.T) {
	mgr := initTestAuditDB(t)

	entry := AuditEntry{
		Account:   "alice",
		Action:    "create filesystem",
		Path:      "/storage/create",
		Success:   false,
		Error:     "disk full",
		CreatedAt: time.Now().UTC(),
	}

	if err := mgr.LogEntry(entry); err != nil {
		t.Fatalf("LogEntry: %v", err)
	}

	page, err := mgr.List(AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}

	got := page.Entries[0]
	if got.Success {
		t.Fatal("expected success to be false")
	}
	if got.Error != "disk full" {
		t.Fatalf("expected error %q, got %q", "disk full", got.Error)
	}
}

// --- Offset pagination ---

func TestAuditListOffsetPagination(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := 0; i < 5; i++ {
		if err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	// First page (offset 0, limit 2) — default desc order
	page, err := mgr.List(AuditListOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}
	if page.Entries[0].Action != "action-4" {
		t.Fatalf("expected action-4, got %s", page.Entries[0].Action)
	}

	// Second page (offset 2, limit 2)
	page2, err := mgr.List(AuditListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page2.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page2.Entries))
	}
	if !page2.HasMore {
		t.Fatal("expected HasMore to be true")
	}
	if page2.Entries[0].Action != "action-2" {
		t.Fatalf("expected action-2, got %s", page2.Entries[0].Action)
	}

	// Last page (offset 4, limit 2)
	page3, err := mgr.List(AuditListOptions{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(page3.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page3.Entries))
	}
	if page3.HasMore {
		t.Fatal("expected HasMore to be false")
	}
	if page3.Entries[0].Action != "action-0" {
		t.Fatalf("expected action-0, got %s", page3.Entries[0].Action)
	}
}

func TestAuditListOffsetWithSort(t *testing.T) {
	mgr := initTestAuditDB(t)

	for _, user := range []string{"charlie", "alice", "bob", "dave", "eve"} {
		if err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	// Page 1 sorted by account asc
	page, err := mgr.List(AuditListOptions{Limit: 2, Offset: 0, SortBy: "account", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Entries[0].Account != "alice" {
		t.Fatalf("expected alice first, got %s", page.Entries[0].Account)
	}
	if page.Entries[1].Account != "bob" {
		t.Fatalf("expected bob second, got %s", page.Entries[1].Account)
	}

	// Page 2 sorted by account asc
	page2, err := mgr.List(AuditListOptions{Limit: 2, Offset: 2, SortBy: "account", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page2.Entries[0].Account != "charlie" {
		t.Fatalf("expected charlie first on page 2, got %s", page2.Entries[0].Account)
	}
	if page2.Entries[1].Account != "dave" {
		t.Fatalf("expected dave second on page 2, got %s", page2.Entries[1].Account)
	}
}

// --- Cursor pagination ---

func TestAuditListCursorPagination(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := 0; i < 5; i++ {
		if err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	// Get first page of 2
	page, err := mgr.List(AuditListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}
	// Entries should be in reverse order (newest first)
	if page.Entries[0].Action != "action-4" {
		t.Fatalf("expected action-4, got %s", page.Entries[0].Action)
	}
	if page.Entries[1].Action != "action-3" {
		t.Fatalf("expected action-3, got %s", page.Entries[1].Action)
	}

	// Get second page using cursor
	cursor := page.Entries[len(page.Entries)-1].ID
	page, err = mgr.List(AuditListOptions{BeforeID: cursor, Limit: 2})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}
	if page.Entries[0].Action != "action-2" {
		t.Fatalf("expected action-2, got %s", page.Entries[0].Action)
	}

	// Get last page
	cursor = page.Entries[len(page.Entries)-1].ID
	page, err = mgr.List(AuditListOptions{BeforeID: cursor, Limit: 2})
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected HasMore to be false")
	}
	if page.Entries[0].Action != "action-0" {
		t.Fatalf("expected action-0, got %s", page.Entries[0].Action)
	}
}

// --- Account filter ---

func TestAuditListAccountFilter(t *testing.T) {
	mgr := initTestAuditDB(t)

	for _, user := range []string{"alice", "bob", "alice"} {
		if err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	page, err := mgr.List(AuditListOptions{Account: "alice"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries for alice, got %d", len(page.Entries))
	}

	for _, e := range page.Entries {
		if e.Account != "alice" {
			t.Fatalf("expected account alice, got %q", e.Account)
		}
	}
}

// --- Default limit ---

func TestAuditListDefaultLimit(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := 0; i < 60; i++ {
		if err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	page, err := mgr.List(AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 50 {
		t.Fatalf("expected default limit 50, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}
}

// --- Max limit clamping ---

func TestAuditListMaxLimitClamping(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := 0; i < 210; i++ {
		if err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	page, err := mgr.List(AuditListOptions{Limit: 500})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 200 {
		t.Fatalf("expected max limit 200, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore to be true")
	}
}

// --- Sort by column ---

func TestAuditListSortByAccountAsc(t *testing.T) {
	mgr := initTestAuditDB(t)

	for _, user := range []string{"charlie", "alice", "bob"} {
		if err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	page, err := mgr.List(AuditListOptions{SortBy: "account", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	expected := []string{"alice", "bob", "charlie"}
	for i, want := range expected {
		if page.Entries[i].Account != want {
			t.Fatalf("index %d: expected account %q, got %q", i, want, page.Entries[i].Account)
		}
	}
}

func TestAuditListSortByAccountDesc(t *testing.T) {
	mgr := initTestAuditDB(t)

	for _, user := range []string{"charlie", "alice", "bob"} {
		if err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	page, err := mgr.List(AuditListOptions{SortBy: "account", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	expected := []string{"charlie", "bob", "alice"}
	for i, want := range expected {
		if page.Entries[i].Account != want {
			t.Fatalf("index %d: expected account %q, got %q", i, want, page.Entries[i].Account)
		}
	}
}

func TestAuditListSortByIDAsc(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := 0; i < 3; i++ {
		if err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	page, err := mgr.List(AuditListOptions{SortBy: "id", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	// Ascending by ID = oldest first
	if page.Entries[0].Action != "action-0" {
		t.Fatalf("expected action-0 first, got %s", page.Entries[0].Action)
	}
	if page.Entries[2].Action != "action-2" {
		t.Fatalf("expected action-2 last, got %s", page.Entries[2].Action)
	}
}

func TestAuditListSortByInvalidColumn(t *testing.T) {
	mgr := initTestAuditDB(t)

	if err := mgr.LogEntry(AuditEntry{
		Account:   "alice",
		Action:    "test",
		Path:      "/test",
		Success:   true,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("LogEntry: %v", err)
	}

	// Invalid column should fall back to "id" default
	page, err := mgr.List(AuditListOptions{SortBy: "DROP TABLE audit_log", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
}

func TestAuditListSortByAction(t *testing.T) {
	mgr := initTestAuditDB(t)

	for _, action := range []string{"create filesystem", "add repository", "disable account"} {
		if err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    action,
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogEntry: %v", err)
		}
	}

	page, err := mgr.List(AuditListOptions{SortBy: "action", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(page.Entries))
	}

	expected := []string{"add repository", "create filesystem", "disable account"}
	for i, want := range expected {
		if page.Entries[i].Action != want {
			t.Fatalf("index %d: expected action %q, got %q", i, want, page.Entries[i].Action)
		}
	}
}

// --- Empty results ---

func TestAuditListEmpty(t *testing.T) {
	mgr := initTestAuditDB(t)

	page, err := mgr.List(AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected HasMore to be false")
	}
}
