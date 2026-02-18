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
