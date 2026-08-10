// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// --- Interface conformance ---

func TestSQLiteSessionManagerImplementsSessionManager(t *testing.T) {
	var _ SessionManager = (*SQLiteSessionManager)(nil)
}

func TestMockSessionManagerImplementsSessionManager(t *testing.T) {
	var _ SessionManager = (*MockSessionManager)(nil)
}

// --- Helpers ---

func initTestSessionDB(t *testing.T) (*SQLiteSessionManager, *SQLiteManager) {
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

	mgr, err := InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	return sessMgr, mgr
}

func createTestUser(t *testing.T, mgr *SQLiteManager, username string) {
	t.Helper()
	_, err := mgr.Create(username, "password", username+"@test.com", "555-0000", "Test User", false)
	if err != nil {
		t.Fatalf("Create user %q: %v", username, err)
	}
}

// --- Create/Validate lifecycle ---

func TestSessionCreateAndValidate(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	sess, acct, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if sess.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", sess.Username)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if acct.Username != "alice" {
		t.Fatalf("expected account username %q, got %q", "alice", acct.Username)
	}
	if acct.PasswordHash != "" {
		t.Fatal("expected password hash to be cleared")
	}
}

func TestSessionValidateInvalidToken(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	_, _, err := sessMgr.Validate(t.Context(), "invalid-token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestSessionValidateRevokedSession(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// get session ID by validating first
	sess, _, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("first Validate: %v", err)
	}

	err = sessMgr.Revoke(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, _, err = sessMgr.Validate(t.Context(), token)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound after revoke, got %v", err)
	}
}

// --- Revoke tests ---

func TestSessionRevoke(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess, _, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	err = sessMgr.Revoke(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after revoke, got %d", len(sessions))
	}
}

func TestSessionRevokeNotFound(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	err := sessMgr.Revoke(t.Context(), "nonexistent-id")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// --- RevokeAllForUser tests ---

func TestSessionRevokeAllForUser(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")
	createTestUser(t, acctMgr, "bob")

	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create alice session 1: %v", err)
	}
	_, err = sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create alice session 2: %v", err)
	}
	_, err = sessMgr.Create(t.Context(), "bob")
	if err != nil {
		t.Fatalf("Create bob session: %v", err)
	}

	err = sessMgr.RevokeAllForUser(t.Context(), "alice")
	if err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	aliceSessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}
	if len(aliceSessions) != 0 {
		t.Fatalf("expected 0 alice sessions, got %d", len(aliceSessions))
	}

	bobSessions, err := sessMgr.List(t.Context(), "bob")
	if err != nil {
		t.Fatalf("List bob: %v", err)
	}
	if len(bobSessions) != 1 {
		t.Fatalf("expected 1 bob session, got %d", len(bobSessions))
	}
}

// --- List tests ---

func TestSessionList(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	_, err = sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestSessionListEmpty(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	sessions, err := sessMgr.List(t.Context(), "nobody")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

// --- GetUsername tests ---

func TestSessionGetUsername(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess, _, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	username, err := sessMgr.GetUsername(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("GetUsername: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", username)
	}
}

func TestSessionGetUsernameNotFound(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	_, err := sessMgr.GetUsername(t.Context(), "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// --- Cleanup tests ---

func TestSessionCleanup(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// manually set last_used to the past
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", cutoff)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	err = sessMgr.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after cleanup, got %d", len(sessions))
	}
}

func TestSessionCleanupPreservesActive(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = sessMgr.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session preserved, got %d", len(sessions))
	}
}

// --- Expiry behavior ---

func TestSessionCreateCleansExpired(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")
	createTestUser(t, acctMgr, "bob")

	// Create sessions for both users.
	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	_, err = sessMgr.Create(t.Context(), "bob")
	if err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	// Expire both sessions by setting last_used to the past.
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", cutoff)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	// Creating a new session should clean up the expired ones.
	_, err = sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create after expiry: %v", err)
	}

	aliceSessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}
	if len(aliceSessions) != 1 {
		t.Fatalf("expected 1 alice session (the new one), got %d", len(aliceSessions))
	}

	bobSessions, err := sessMgr.List(t.Context(), "bob")
	if err != nil {
		t.Fatalf("List bob: %v", err)
	}
	if len(bobSessions) != 0 {
		t.Fatalf("expected 0 bob sessions (expired should be cleaned), got %d", len(bobSessions))
	}
}

func TestSessionExpired(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// manually set last_used to the past
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", cutoff)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	_, _, err = sessMgr.Validate(t.Context(), token)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

// --- Multiple sessions per user ---

func TestMultipleSessionsPerUser(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token1, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	token2, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	// both should be valid
	_, _, err = sessMgr.Validate(t.Context(), token1)
	if err != nil {
		t.Fatalf("Validate token1: %v", err)
	}
	_, _, err = sessMgr.Validate(t.Context(), token2)
	if err != nil {
		t.Fatalf("Validate token2: %v", err)
	}

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

// --- Cascade disable ---

func TestSessionCascadeDisableOnAccountDisable(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	_, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	err = acctMgr.Disable("alice")
	if err != nil {
		t.Fatalf("Disable account: %v", err)
	}

	// sessions still exist but account is disabled
	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after account disable, got %d", len(sessions))
	}
}

// --- Repeated validation (session must survive) ---

func TestSessionRepeatedValidation(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := range 100 {
		sess, acct, err := sessMgr.Validate(t.Context(), token)
		if err != nil {
			t.Fatalf("Validate iteration %d: %v", i, err)
		}
		if sess.Username != "alice" {
			t.Fatalf("iteration %d: expected username %q, got %q", i, "alice", sess.Username)
		}
		if acct.Username != "alice" {
			t.Fatalf("iteration %d: expected account %q, got %q", i, "alice", acct.Username)
		}
	}
}

// --- Concurrent validation ---

func TestSessionConcurrentValidation(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const goroutines = 20
	const iterations = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*iterations)

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				_, _, err := sessMgr.Validate(t.Context(), token)
				if err != nil {
					errs <- err
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	var allErrs []error
	for err := range errs {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) > 0 {
		t.Fatalf("concurrent validation produced %d errors; first: %v", len(allErrs), allErrs[0])
	}
}

// --- Validate updates last_used ---

func TestSessionValidateUpdatesLastUsed(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess1, _, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("Validate 1: %v", err)
	}

	// Set last_used to 1 hour ago to avoid relying on wall-clock timing.
	oneHourAgo := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ? WHERE id = ?", oneHourAgo, sess1.ID)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	sess2, _, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("Validate 2: %v", err)
	}

	if !sess2.LastUsed.After(sess1.LastUsed) {
		t.Fatalf("expected last_used to advance: first=%v second=%v", sess1.LastUsed, sess2.LastUsed)
	}
}

// --- Session does not expire prematurely ---

func TestSessionNotExpiredPrematurely(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Set last_used to 6 days ago (just under the 7-day max age).
	sixDaysAgo := time.Now().UTC().Add(-6 * 24 * time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", sixDaysAgo)
	if err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	sess, _, err := sessMgr.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf("session should still be valid at 6 days, got: %v", err)
	}
	if sess.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", sess.Username)
	}
}

// --- Concurrent validation with mixed operations ---

func TestSessionConcurrentMixedOperations(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")
	createTestUser(t, acctMgr, "bob")

	aliceToken, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create alice: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*100)

	// Concurrent validates on alice's token.
	for range goroutines / 2 {
		wg.Go(func() {
			for range 50 {
				_, _, err := sessMgr.Validate(t.Context(), aliceToken)
				if err != nil {
					errs <- fmt.Errorf("validate alice: %w", err)
				}
			}
		})
	}

	// Concurrent creates for bob (triggers Cleanup).
	for range goroutines / 2 {
		wg.Go(func() {
			for range 10 {
				_, err := sessMgr.Create(t.Context(), "bob")
				if err != nil {
					errs <- fmt.Errorf("create bob: %w", err)
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	var allErrs []error
	for err := range errs {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) > 0 {
		t.Fatalf("concurrent mixed ops produced %d errors; first: %v", len(allErrs), allErrs[0])
	}

	// Alice's session should still be valid.
	sess, _, err := sessMgr.Validate(t.Context(), aliceToken)
	if err != nil {
		t.Fatalf("alice session should survive concurrent ops: %v", err)
	}
	if sess.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", sess.Username)
	}
}

// --- Cleanup does not remove active sessions ---

func TestSessionCleanupDoesNotRemoveRecentSessions(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")
	createTestUser(t, acctMgr, "bob")

	aliceToken, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	bobToken, err := sessMgr.Create(t.Context(), "bob")
	if err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	// Run cleanup multiple times.
	for range 10 {
		err := sessMgr.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	}

	// Both sessions should still be valid.
	_, _, err = sessMgr.Validate(t.Context(), aliceToken)
	if err != nil {
		t.Fatalf("alice session should survive cleanup: %v", err)
	}
	_, _, err = sessMgr.Validate(t.Context(), bobToken)
	if err != nil {
		t.Fatalf("bob session should survive cleanup: %v", err)
	}
}

// --- Validate with errors.Is ---

func TestSessionErrorsIs(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	// Invalid token
	_, _, err := sessMgr.Validate(t.Context(), "garbage")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected errors.Is(err, ErrInvalidToken), got %v", err)
	}

	// Expired session
	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	expired := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	_, err = sessMgr.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ?", expired)
	if err != nil {
		t.Fatalf("manual update: %v", err)
	}
	_, _, err = sessMgr.Validate(t.Context(), token)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected errors.Is(err, ErrSessionExpired), got %v", err)
	}

	// Revoked session
	token2, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	sess, _, err := sessMgr.Validate(t.Context(), token2)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	err = sessMgr.Revoke(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, _, err = sessMgr.Validate(t.Context(), token2)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected errors.Is(err, ErrSessionNotFound), got %v", err)
	}
}

// --- InitSessionManager clears all sessions ---

func TestInitSessionManagerClearsExistingSessions(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	createTestUser(t, mgr, "alice")
	createTestUser(t, mgr, "bob")

	_, err = sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	_, err = sessMgr.Create(t.Context(), "bob")
	if err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	sessions, err := sessMgr.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 alice session before reinit, got %d", len(sessions))
	}

	// Re-initialize the session manager (simulates service restart).
	newKey := []byte("new-signing-key-for-sessions-32!")
	sessMgr2, err := InitSessionManager(t.Context(), db, mgr, newKey)
	if err != nil {
		t.Fatalf("InitSessionManager (reinit): %v", err)
	}

	// All sessions should be cleared.
	aliceSessions, err := sessMgr2.List(t.Context(), "alice")
	if err != nil {
		t.Fatalf("List alice after reinit: %v", err)
	}
	if len(aliceSessions) != 0 {
		t.Fatalf("expected 0 alice sessions after reinit, got %d", len(aliceSessions))
	}

	bobSessions, err := sessMgr2.List(t.Context(), "bob")
	if err != nil {
		t.Fatalf("List bob after reinit: %v", err)
	}
	if len(bobSessions) != 0 {
		t.Fatalf("expected 0 bob sessions after reinit, got %d", len(bobSessions))
	}
}

// --- Wrong signing key ---

func TestSessionValidateWrongSigningKey(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// create a new session manager with a different key
	sessMgr2, err := InitSessionManager(t.Context(), sessMgr.db, acctMgr, []byte("different-signing-key-1234567890"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	_, _, err = sessMgr2.Validate(t.Context(), token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken with wrong key, got %v", err)
	}
}
