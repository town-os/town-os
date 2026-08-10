// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"context"
	"testing"
	"time"
)

// --- HasActiveAdminSessions ---

func TestHasActiveAdminSessionsWithActiveSessions(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "admin1")
	createTestUser(t, acctMgr, "admin2")
	createTestUser(t, acctMgr, "regularuser")

	_, err := sessMgr.Create(t.Context(), "admin1")
	if err != nil {
		t.Fatalf("Create admin1 session: %v", err)
	}

	has, err := sessMgr.HasActiveAdminSessions(t.Context(), []string{"admin1", "admin2"})
	if err != nil {
		t.Fatalf("HasActiveAdminSessions: %v", err)
	}
	if !has {
		t.Fatal("expected true when admin1 has an active session")
	}
}

func TestHasActiveAdminSessionsNoActiveSessions(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "admin1")

	has, err := sessMgr.HasActiveAdminSessions(t.Context(), []string{"admin1"})
	if err != nil {
		t.Fatalf("HasActiveAdminSessions: %v", err)
	}
	if has {
		t.Fatal("expected false when no admin sessions exist")
	}
}

func TestHasActiveAdminSessionsEmptyList(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	has, err := sessMgr.HasActiveAdminSessions(t.Context(), []string{})
	if err != nil {
		t.Fatalf("HasActiveAdminSessions: %v", err)
	}
	if has {
		t.Fatal("expected false for empty admin list")
	}
}

func TestHasActiveAdminSessionsExpiredCleaned(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "admin1")

	_, err := sessMgr.Create(t.Context(), "admin1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Expire the session.
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", cutoff)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	has, err := sessMgr.HasActiveAdminSessions(t.Context(), []string{"admin1"})
	if err != nil {
		t.Fatalf("HasActiveAdminSessions: %v", err)
	}
	if has {
		t.Fatal("expected false after session expired and cleanup ran")
	}
}

func TestHasActiveAdminSessionsOnlyNonAdminSessions(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "admin1")
	createTestUser(t, acctMgr, "regularuser")

	_, err := sessMgr.Create(t.Context(), "regularuser")
	if err != nil {
		t.Fatalf("Create regularuser: %v", err)
	}

	has, err := sessMgr.HasActiveAdminSessions(t.Context(), []string{"admin1"})
	if err != nil {
		t.Fatalf("HasActiveAdminSessions: %v", err)
	}
	if has {
		t.Fatal("expected false when only non-admin sessions exist")
	}
}

// --- StartCleanup ---

func TestStartCleanupRunsAndStops(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Expire the session.
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", cutoff)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sessMgr.StartCleanup(ctx, 50*time.Millisecond)

	// Wait for at least one cleanup cycle.
	time.Sleep(200 * time.Millisecond)

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after cleanup, got %d", len(sessions))
	}

	// Cancel should stop the goroutine without error.
	cancel()
	time.Sleep(100 * time.Millisecond) // allow goroutine to exit
}

func TestStartCleanupContextCancellation(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	sessMgr.StartCleanup(ctx, time.Hour) // long interval

	// Cancel immediately — goroutine should exit cleanly.
	cancel()
	time.Sleep(100 * time.Millisecond)
}
