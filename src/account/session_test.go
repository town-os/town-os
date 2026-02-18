package account

import (
	"path/filepath"
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
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	return sessMgr, mgr
}

func createTestUser(t *testing.T, mgr *SQLiteManager, username string) {
	t.Helper()
	if _, err := mgr.Create(username, "password", username+"@test.com", "555-0000", "Test User", false); err != nil {
		t.Fatalf("Create user %q: %v", username, err)
	}
}

// --- Create/Validate lifecycle ---

func TestSessionCreateAndValidate(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	sess, acct, err := sessMgr.Validate(token)
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

	_, _, err := sessMgr.Validate("invalid-token")
	if err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestSessionValidateRevokedSession(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// get session ID by validating first
	sess, _, err := sessMgr.Validate(token)
	if err != nil {
		t.Fatalf("first Validate: %v", err)
	}

	if err := sessMgr.Revoke(sess.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, _, err = sessMgr.Validate(token)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound after revoke, got %v", err)
	}
}

// --- Revoke tests ---

func TestSessionRevoke(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess, _, err := sessMgr.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if err := sessMgr.Revoke(sess.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	sessions, err := sessMgr.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after revoke, got %d", len(sessions))
	}
}

func TestSessionRevokeNotFound(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	err := sessMgr.Revoke("nonexistent-id")
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// --- RevokeAllForUser tests ---

func TestSessionRevokeAllForUser(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")
	createTestUser(t, acctMgr, "bob")

	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create alice session 1: %v", err)
	}
	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create alice session 2: %v", err)
	}
	if _, err := sessMgr.Create("bob"); err != nil {
		t.Fatalf("Create bob session: %v", err)
	}

	if err := sessMgr.RevokeAllForUser("alice"); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	aliceSessions, err := sessMgr.List("alice")
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}
	if len(aliceSessions) != 0 {
		t.Fatalf("expected 0 alice sessions, got %d", len(aliceSessions))
	}

	bobSessions, err := sessMgr.List("bob")
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

	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	sessions, err := sessMgr.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestSessionListEmpty(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	sessions, err := sessMgr.List("nobody")
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

	token, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess, _, err := sessMgr.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	username, err := sessMgr.GetUsername(sess.ID)
	if err != nil {
		t.Fatalf("GetUsername: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", username)
	}
}

func TestSessionGetUsernameNotFound(t *testing.T) {
	sessMgr, _ := initTestSessionDB(t)

	_, err := sessMgr.GetUsername("nonexistent")
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// --- Cleanup tests ---

func TestSessionCleanup(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// manually set last_used to the past
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	if _, err := sessMgr.db.Exec("UPDATE sessions SET last_used = ?", cutoff); err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	if err := sessMgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	sessions, err := sessMgr.List("alice")
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

	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sessMgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	sessions, err := sessMgr.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session preserved, got %d", len(sessions))
	}
}

// --- Expiry behavior ---

func TestSessionExpired(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// manually set last_used to the past
	cutoff := time.Now().UTC().Add(-SessionMaxAge - time.Hour).Format(time.RFC3339)
	if _, err := sessMgr.db.Exec("UPDATE sessions SET last_used = ?", cutoff); err != nil {
		t.Fatalf("manual update last_used: %v", err)
	}

	_, _, err = sessMgr.Validate(token)
	if err != ErrSessionExpired {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

// --- Multiple sessions per user ---

func TestMultipleSessionsPerUser(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token1, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	token2, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	// both should be valid
	if _, _, err := sessMgr.Validate(token1); err != nil {
		t.Fatalf("Validate token1: %v", err)
	}
	if _, _, err := sessMgr.Validate(token2); err != nil {
		t.Fatalf("Validate token2: %v", err)
	}

	sessions, err := sessMgr.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

// --- Cascade delete ---

func TestSessionCascadeDeleteOnAccountDelete(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	if _, err := sessMgr.Create("alice"); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	if err := acctMgr.Delete("alice"); err != nil {
		t.Fatalf("Delete account: %v", err)
	}

	sessions, err := sessMgr.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after account delete, got %d", len(sessions))
	}
}

// --- Wrong signing key ---

func TestSessionValidateWrongSigningKey(t *testing.T) {
	sessMgr, acctMgr := initTestSessionDB(t)
	createTestUser(t, acctMgr, "alice")

	token, err := sessMgr.Create("alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// create a new session manager with a different key
	sessMgr2, err := InitSessionManager(sessMgr.db, acctMgr, []byte("different-signing-key-1234567890"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	_, _, err = sessMgr2.Validate(token)
	if err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken with wrong key, got %v", err)
	}
}
