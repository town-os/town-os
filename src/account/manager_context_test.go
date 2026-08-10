package account

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// The SQLite managers used to open their own 30-second root context per query,
// so a caller's cancellation stopped at the manager boundary: an abandoned HTTP
// request kept working and graceful shutdown could not interrupt one. With
// SetMaxOpenConns(1) — SQLite allows one writer, so every query is serialized
// behind that single connection — a slow query held every other caller behind
// an uninterruptible 30-second ceiling.
//
// These tests are what make the context a real parameter rather than one that
// is accepted and dropped.

func newSettingsForCtxTest(t *testing.T) *SQLiteSettingsManager {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}
	return mgr
}

func TestSettingsGetHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	mgr := newSettingsForCtxTest(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := mgr.Get(ctx, "locale"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestSettingsSetHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	mgr := newSettingsForCtxTest(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := mgr.Set(ctx, "locale", "de-DE"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Set with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestSettingsListHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	mgr := newSettingsForCtxTest(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := mgr.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List with a cancelled context = %v, want context.Canceled", err)
	}
}

// A caller's deadline wins when it is shorter than dbTimeout. Without this,
// dbTimeout is not a ceiling but the only timeout there is, and a handler with
// a one-second budget still waits thirty.
func TestSettingsHonorsCallerDeadlineShorterThanDBTimeout(t *testing.T) {
	t.Parallel()
	mgr := newSettingsForCtxTest(t)

	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // let it elapse

	if _, err := mgr.Get(ctx, "locale"); err == nil {
		t.Fatal("Get with an elapsed caller deadline succeeded; the caller's deadline is not being applied")
	}
}

// And dbTimeout still bounds a caller that brought no deadline of its own, so
// nothing can hang forever on the single connection.
func TestSettingsAppliesCeilingToContextWithoutDeadline(t *testing.T) {
	t.Parallel()
	mgr := newSettingsForCtxTest(t)

	if err := mgr.Set(t.Context(), "locale", "en-US"); err != nil {
		t.Fatalf("Set with a plain context: %v", err)
	}

	ctx, cancel := queryCtx(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("queryCtx produced a context with no deadline")
	}
	if remaining := time.Until(deadline); remaining > dbTimeout {
		t.Errorf("queryCtx deadline is %v away, longer than the %v ceiling", remaining, dbTimeout)
	}
}

// A nil context is treated as Background rather than panicking. A manager is
// the wrong layer to take a box down over an argument its caller forgot, and
// tests that construct handlers directly leave the server context nil.
func TestQueryCtxToleratesNilContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := queryCtx(nil) //nolint:staticcheck // SA1012 -- tolerating nil is the behavior under test
	defer cancel()

	if ctx == nil {
		t.Fatal("queryCtx(nil) returned a nil context")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Error("queryCtx(nil) produced a context with no deadline")
	}
}

// --- AuditManager ---

func newAuditForCtxTest(t *testing.T) *SQLiteAuditManager {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}
	return mgr
}

func TestAuditHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	mgr := newAuditForCtxTest(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	entry := AuditEntry{Account: "alice", Action: "test", Path: "/test", Success: true, CreatedAt: time.Now().UTC()}
	if err := mgr.LogEntry(ctx, entry); !errors.Is(err, context.Canceled) {
		t.Errorf("LogEntry with a cancelled context = %v, want context.Canceled", err)
	}
	if _, err := mgr.List(ctx, AuditListOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("List with a cancelled context = %v, want context.Canceled", err)
	}
	if _, err := mgr.CountRecentErrors(ctx, time.Now().Add(-time.Hour)); !errors.Is(err, context.Canceled) {
		t.Errorf("CountRecentErrors with a cancelled context = %v, want context.Canceled", err)
	}
}

// auditMiddleware passes the SERVER context to LogEntry, not the request's,
// and this is what that buys: an audited action is recorded even when the
// client that triggered it has already gone away. Passing the request context
// would let hanging up cancel the write that records the request.
func TestAuditLogEntrySucceedsAfterRequestContextIsCancelled(t *testing.T) {
	t.Parallel()
	mgr := newAuditForCtxTest(t)

	// Stand in for the request context the handler ran under.
	reqCtx, cancelReq := context.WithCancel(t.Context())
	cancelReq()

	// The middleware uses the server context instead, which is still live.
	entry := AuditEntry{Account: "alice", Action: "install", Path: "/packages/install", Success: true, CreatedAt: time.Now().UTC()}
	if err := mgr.LogEntry(t.Context(), entry); err != nil {
		t.Fatalf("LogEntry on the server context after the request was cancelled: %v", err)
	}

	page, err := mgr.List(t.Context(), AuditListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("audit entries = %d, want 1; the record of the action was lost", len(page.Entries))
	}

	// And the request context really was dead, so the test is not vacuous.
	if reqCtx.Err() == nil {
		t.Fatal("request context was expected to be cancelled")
	}
}
