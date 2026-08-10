// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"github.com/labstack/echo/v5"
)

// newAuthTestManagers builds a real account + session manager pair on a
// throwaway database. Only the session manager's presence matters to these
// tests, but it has to be a real one: InitTestServer keys on the field, and a
// nil-typed interface holding a nil pointer would read as present.
func newAuthTestManagers(t *testing.T) (account.Manager, account.SessionManager) {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	return mgr, sessMgr
}

// The authorization layer used to treat "there is no session manager" as
// "authentication is not configured, so let it through". requireAuth,
// requireAdmin, requireGrant, revokeSession, requireNetworkScope, and
// callerIsAdmin all read that one nil the same way.
//
// Nothing in production ever left it nil, so this was never exploitable — but
// the whole authorization surface sat one unset field away from admitting an
// anonymous caller to /account/create and /packages/install, on a controller
// that drives the host podman socket as root, with nothing in the type system
// saying so and no error anywhere if it happened.
//
// The condition is now ServerConfig.AuthDisabled: stated, not inferred. A
// missing session manager with auth enabled is a misconfigured box, and every
// one of these paths refuses it.

// authTestHandlers builds a handler set whose backend has no session manager
// and has NOT asked for auth to be disabled — the state that used to be wide
// open.
func authTestHandlers() *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: &serverBase{ServerConfig: ServerConfig{}}}
}

func authTestContext(t *testing.T) *echo.Context {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	return e.NewContext(req, httptest.NewRecorder())
}

// reached records whether the middleware called through to the route.
func reachedHandler(hit *bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		*hit = true
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}
}

func TestRequireAuthRefusesWhenSessionManagerMissing(t *testing.T) {
	s := authTestHandlers()

	hit := false
	err := s.requireAuth(reachedHandler(&hit))(authTestContext(t))

	if hit {
		t.Fatal("requireAuth called through to the route with no session manager: the route was served to an anonymous caller")
	}
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("requireAuth error = %v, want an *echo.HTTPError", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Errorf("requireAuth status = %d, want 500 (a misconfigured box, not a caller error)", httpErr.Code)
	}
}

func TestRequireAdminRefusesWhenSessionManagerMissing(t *testing.T) {
	s := authTestHandlers()

	hit := false
	err := s.requireAdmin(reachedHandler(&hit))(authTestContext(t))

	if hit {
		t.Fatal("requireAdmin called through to the route with no session manager: an admin-only route was served to an anonymous caller")
	}
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("requireAdmin error = %v, want an *echo.HTTPError", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Errorf("requireAdmin status = %d, want 500", httpErr.Code)
	}
}

// requireGrant guards peer enrollment and the object-storage mutators. It read
// the same nil, so it admitted an anonymous caller to routes a grant is
// supposed to be required for.
func TestRequireGrantRefusesWhenSessionManagerMissing(t *testing.T) {
	s := authTestHandlers()

	hit := false
	err := s.requirePeerEnroll(reachedHandler(&hit))(authTestContext(t))

	if hit {
		t.Fatal("requirePeerEnroll called through to the route with no session manager")
	}
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("requirePeerEnroll error = %v, want an *echo.HTTPError", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Errorf("requirePeerEnroll status = %d, want 500", httpErr.Code)
	}
}

// callerIsAdmin decides how much a handler discloses. Answering true for an
// unidentifiable caller is the disclosure, not the refusal.
func TestCallerIsAdminFalseWhenSessionManagerMissing(t *testing.T) {
	s := authTestHandlers()

	if s.callerIsAdmin(authTestContext(t)) {
		t.Error("callerIsAdmin = true with no session manager: an unidentified caller is treated as an administrator")
	}
}

func TestCallerIsAdminTrueWhenAuthExplicitlyDisabled(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &serverBase{ServerConfig: ServerConfig{AuthDisabled: true}}}

	if !s.callerIsAdmin(authTestContext(t)) {
		t.Error("callerIsAdmin = false with auth disabled: a handler must not redact its way to uselessness on a box that deliberately has no auth")
	}
}

// A box that asked for no auth still serves. This is the case the ~230
// InitTestServer call sites depend on, so it has to keep working.
func TestAuthDisabledStillServes(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &serverBase{ServerConfig: ServerConfig{AuthDisabled: true}}}

	for name, mw := range map[string]func(echo.HandlerFunc) echo.HandlerFunc{
		"requireAuth":       s.requireAuth,
		"requireAdmin":      s.requireAdmin,
		"requirePeerEnroll": s.requirePeerEnroll,
	} {
		hit := false
		if err := mw(reachedHandler(&hit))(authTestContext(t)); err != nil {
			t.Errorf("%s with auth disabled returned %v, want nil", name, err)
		}
		if !hit {
			t.Errorf("%s did not call through with auth disabled", name)
		}
	}
}

// requireNetworkScope confines a grant-holding account to its own networks. It
// read the same nil and returned "no confinement".
func TestRequireNetworkScopeNoLongerKeysOnSessionManager(t *testing.T) {
	s := authTestHandlers()

	// With no token there is no calling account, so there is nobody to
	// confine and nil is still correct here — the route's own middleware is
	// what refuses. The point of this test is that the decision no longer
	// depends on the session manager being nil.
	if err := s.requireNetworkScope(authTestContext(t), "office"); err != nil {
		t.Errorf("requireNetworkScope for an unauthenticated caller = %v, want nil (the route middleware refuses, not this)", err)
	}
}

func TestNewHandlerRefusesWithoutSessionManager(t *testing.T) {
	handler, err := NewHandler(t.Context(), ServerConfig{})

	if !errors.Is(err, ErrAuthNotConfigured) {
		t.Fatalf("NewHandler error = %v, want ErrAuthNotConfigured", err)
	}
	if handler != nil {
		t.Error("NewHandler returned a handler alongside the error: a caller ignoring err would serve an unauthenticated box")
	}
}

func TestNewHandlerAcceptsExplicitAuthDisabled(t *testing.T) {
	handler, err := NewHandler(t.Context(), ServerConfig{AuthDisabled: true})
	if err != nil {
		t.Fatalf("NewHandler with AuthDisabled: %v", err)
	}
	if handler == nil {
		t.Error("NewHandler returned no handler for an explicitly unauthenticated box")
	}
}

// InitTestServer derives AuthDisabled so the existing test corpus is untouched.
// It must derive it only from the absence of a session manager: a test that
// installs one is testing auth, and silently disabling it there would turn
// every authorization assertion in the suite into a tautology.
func TestInitTestServerDerivesAuthDisabled(t *testing.T) {
	ts := InitTestServer(ServerConfig{})
	t.Cleanup(ts.Close)

	if !ts.AuthDisabled {
		t.Error("InitTestServer did not disable auth for a config with no session manager")
	}
}

func TestInitTestServerKeepsAuthWithSessionManager(t *testing.T) {
	_, sessMgr := newAuthTestManagers(t)

	ts := InitTestServer(ServerConfig{SessionMgr: sessMgr})
	t.Cleanup(ts.Close)

	if ts.AuthDisabled {
		t.Error("InitTestServer disabled auth for a config that installs a session manager: every auth assertion in the suite would become a tautology")
	}
}
