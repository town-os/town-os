// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// POST /account/session/revoke is registered with NO middleware, under the
// comment "Self-authenticated (handlers do own token validation)":
//
//	e.Add("POST", "/account/session/revoke", s.revokeSession)
//
// Its two neighbours on that list (listSessions, sessionUsername) do exactly
// that -- extractBearerToken, then Validate. revokeSession does neither. It
// decodes session_id from the body and deletes the row:
//
//	if err := s.Controller.GetSessionManager().Revoke(req.SessionID); err != nil {
//
// Nothing upstream covers it either: grantAllowlist passes an unidentifiable
// caller straight through, and the audit middleware does not authenticate. So
// the endpoint is an unauthenticated write, reachable before login, that
// terminates any session whose id the caller knows -- and GET /account/sessions
// hands a user their own ids, which are the ones an attacker would replay after
// a session is shared, logged, or read off a screen.
//
// Session ids are UUIDv4, so this is not blind-guessable; that bounds the
// severity, not the defect.
//
// These tests assert the SECURE behaviour and fail against the current code.

type revokeAuthzEnv struct {
	client *systemcontroller.SystemdClient

	victimToken     string
	victimSessionID string
	otherToken      string
}

func initRevokeAuthzTest(t *testing.T) *revokeAuthzEnv {
	t.Helper()

	db, err := account.OpenDB(t.Context(), filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})

	mgr, err := account.InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFSMock(),
		AccountMgr: mgr,
		SessionMgr: sessMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// The victim is the box's administrator: revoking its session is the most
	// consequential form of this, and it is the account an attacker would aim at.
	if _, err := c.CreateAccount(context.TODO(), "victim", "victimpass1", "v@test.com", "555-0000", "Victim", true); err != nil {
		t.Fatalf("bootstrap CreateAccount victim: %v", err)
	}
	victimResp, err := c.Authenticate(context.TODO(), "victim", "victimpass1")
	if err != nil {
		t.Fatalf("Authenticate victim: %v", err)
	}
	c.Token = victimResp.Token

	// An ordinary second account, for the "some other logged-in user" case.
	if _, err := c.CreateAccount(context.TODO(), "other", "otherpass1", "o@test.com", "555-0001", "Other", false); err != nil {
		t.Fatalf("CreateAccount other: %v", err)
	}
	otherResp, err := c.Authenticate(context.TODO(), "other", "otherpass1")
	if err != nil {
		t.Fatalf("Authenticate other: %v", err)
	}

	sessions, err := c.ListSessions(context.TODO(), victimResp.Token)
	if err != nil {
		t.Fatalf("ListSessions victim: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 victim session, got %d", len(sessions))
	}

	return &revokeAuthzEnv{
		client:          c,
		victimToken:     victimResp.Token,
		victimSessionID: sessions[0].ID,
		otherToken:      otherResp.Token,
	}
}

// revoke posts to /account/session/revoke with the given bearer token (empty
// means no Authorization header at all) and returns the status.
func (e *revokeAuthzEnv) revoke(t *testing.T, token, sessionID string) (int, string) {
	t.Helper()
	body := `{"session_id":` + jsonQuote(t, sessionID) + `}`
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost,
		e.client.BaseURL+"/account/session/revoke", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build revoke request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.client.HTTP.Do(req)
	if err != nil {
		t.Fatalf("POST account/session/revoke: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read revoke body: %v", err)
	}
	return resp.StatusCode, string(out)
}

// tokenStillWorks reports whether a bearer token is still accepted, asked
// through GET /account/me because it is the cheapest authenticated route and
// carries no side effects.
func (e *revokeAuthzEnv) tokenStillWorks(t *testing.T, token string) bool {
	t.Helper()
	_, err := e.client.SessionUsername(context.TODO(), token)
	return err == nil
}

func TestSessionRevokeRequiresAuthentication(t *testing.T) {
	t.Parallel()
	e := initRevokeAuthzTest(t)

	code, body := e.revoke(t, "", e.victimSessionID)
	if code != http.StatusUnauthorized {
		t.Errorf("unauthenticated POST /account/session/revoke = %d (%s), want 401", code, body)
	}

	if !e.tokenStillWorks(t, e.victimToken) {
		t.Error("an unauthenticated caller revoked the administrator's live session")
	}
}

// A garbage token must be no better than none. Without a Validate call the
// handler cannot tell them apart -- it never looks at the header.
func TestSessionRevokeRejectsInvalidToken(t *testing.T) {
	t.Parallel()
	e := initRevokeAuthzTest(t)

	code, body := e.revoke(t, "not-a-real-token", e.victimSessionID)
	if code != http.StatusUnauthorized {
		t.Errorf("POST /account/session/revoke with a garbage token = %d (%s), want 401", code, body)
	}

	if !e.tokenStillWorks(t, e.victimToken) {
		t.Error("a caller holding an invalid token revoked the administrator's live session")
	}
}

// Authenticating as somebody else is not authorization to end this session.
// A session belongs to one account; ListSessions is already scoped that way.
func TestSessionRevokeRejectsAnotherAccountsSession(t *testing.T) {
	t.Parallel()
	e := initRevokeAuthzTest(t)

	code, body := e.revoke(t, e.otherToken, e.victimSessionID)
	if code == http.StatusOK {
		t.Errorf("POST /account/session/revoke let %q end another account's session (status %d, body %s)",
			"other", code, body)
	}

	if !e.tokenStillWorks(t, e.victimToken) {
		t.Error("an unrelated authenticated account revoked the administrator's live session")
	}
}

// The counterpart: revoking your OWN session must keep working, which is what
// the route is for. Without this a fix could simply deny everything.
func TestSessionRevokeOwnSessionStillWorks(t *testing.T) {
	t.Parallel()
	e := initRevokeAuthzTest(t)

	code, body := e.revoke(t, e.victimToken, e.victimSessionID)
	if code != http.StatusOK {
		t.Fatalf("revoking one's own session = %d (%s), want 200", code, body)
	}

	if e.tokenStillWorks(t, e.victimToken) {
		t.Error("token still valid after the session behind it was revoked")
	}
}
