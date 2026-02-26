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
		err := db.Close()
		if err != nil {
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

	err := mgr.LogEntry(entry)
	if err != nil {
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

	err := mgr.LogEntry(entry)
	if err != nil {
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

	for i := range 5 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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
		err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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

	for i := range 5 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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
		err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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

	for i := range 60 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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

	for i := range 210 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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
		err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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
		err := mgr.LogEntry(AuditEntry{
			Account:   user,
			Action:    "test",
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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

	for i := range 3 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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

	err := mgr.LogEntry(AuditEntry{
		Account:   "alice",
		Action:    "test",
		Path:      "/test",
		Success:   true,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
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
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    action,
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
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

// --- Migration: duplicate column ---

func TestInitAuditManagerTwice(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		err := db.Close()
		if err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	// First init creates the table and columns.
	_, err = InitAuditManager(db)
	if err != nil {
		t.Fatalf("first InitAuditManager: %v", err)
	}

	// Second init should succeed: the "duplicate column" error from
	// ALTER TABLE ADD COLUMN should be silently tolerated.
	_, err = InitAuditManager(db)
	if err != nil {
		t.Fatalf("second InitAuditManager: %v", err)
	}
}

// --- Detail field ---

func TestAuditLogEntryDetail(t *testing.T) {
	mgr := initTestAuditDB(t)

	entry := AuditEntry{
		Account:   "alice",
		Action:    "disable account",
		Path:      "/account/disable",
		Detail:    `{"username":"bob"}`,
		Success:   true,
		CreatedAt: time.Now().UTC(),
	}

	err := mgr.LogEntry(entry)
	if err != nil {
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
	if got.Detail != `{"username":"bob"}` {
		t.Fatalf("expected detail %q, got %q", `{"username":"bob"}`, got.Detail)
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

// --- TotalPages tests ---

func TestAuditTotalPagesEmpty(t *testing.T) {
	mgr := initTestAuditDB(t)

	page, err := mgr.List(AuditListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if page.TotalPages != 1 {
		t.Fatalf("expected TotalPages=1 for empty table, got %d", page.TotalPages)
	}
}

func TestAuditTotalPagesExactFit(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := range 4 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	page, err := mgr.List(AuditListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if page.TotalPages != 2 {
		t.Fatalf("expected TotalPages=2 for 4 entries with limit 2, got %d", page.TotalPages)
	}
}

func TestAuditTotalPagesPartialLastPage(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := range 5 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "bob",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	page, err := mgr.List(AuditListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if page.TotalPages != 3 {
		t.Fatalf("expected TotalPages=3 for 5 entries with limit 2, got %d", page.TotalPages)
	}
}

func TestAuditTotalPagesWithAccountFilter(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := range 3 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("alice-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("LogEntry alice %d: %v", i, err)
		}
	}

	for i := range 7 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "bob",
			Action:    fmt.Sprintf("bob-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("LogEntry bob %d: %v", i, err)
		}
	}

	// Without filter: 10 entries / 5 per page = 2 pages
	page, err := mgr.List(AuditListOptions{Limit: 5})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if page.TotalPages != 2 {
		t.Fatalf("expected TotalPages=2 for 10 entries with limit 5, got %d", page.TotalPages)
	}

	// With account filter: 3 alice entries / 5 per page = 1 page
	page2, err := mgr.List(AuditListOptions{Limit: 5, Account: "alice"})
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}
	if page2.TotalPages != 1 {
		t.Fatalf("expected TotalPages=1 for 3 alice entries with limit 5, got %d", page2.TotalPages)
	}
}

func TestAuditTotalPagesConsistentAcrossPages(t *testing.T) {
	mgr := initTestAuditDB(t)

	for i := range 5 {
		err := mgr.LogEntry(AuditEntry{
			Account:   "alice",
			Action:    fmt.Sprintf("action-%d", i),
			Path:      "/test",
			Success:   true,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("LogEntry %d: %v", i, err)
		}
	}

	page1, err := mgr.List(AuditListOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}

	page2, err := mgr.List(AuditListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}

	page3, err := mgr.List(AuditListOptions{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}

	if page1.TotalPages != 3 || page2.TotalPages != 3 || page3.TotalPages != 3 {
		t.Fatalf("expected TotalPages=3 on all pages, got %d, %d, %d",
			page1.TotalPages, page2.TotalPages, page3.TotalPages)
	}
}
