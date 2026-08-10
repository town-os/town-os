// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// Routes that were readable or writable by any account with a login, and are
// not any more.
//
// Two shapes are covered here. The plainly authenticated ones are driven over
// the httptest server. The localhost-gated ones (the journal, the audit log)
// cannot be: httptest connects from 127.0.0.1, so isLocalhost passes every
// request through before the admin check is reached. Those are driven against
// the router directly with a non-loopback RemoteAddr, which is the only way to
// exercise the branch a LAN client actually takes.

// authEnv is a ServerConfig with real account, session, and audit stores, plus
// tokens for an administrator and a plain user.
//
// Tokens come from the session manager directly rather than from an
// authenticate round trip: these tests are about what a token may reach, and
// minting one through the manager keeps them independent of the login path
// (which has its own throttle, and would otherwise be exercised here by
// accident).
type authEnv struct {
	cfg        ServerConfig
	adminToken string
	userToken  string
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	auditMgr, err := account.InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	if _, err := mgr.Create("admin", authTestPassword, "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("Create admin: %v", err)
	}
	if _, err := mgr.Create("user", authTestPassword, "user@b.com", "555", "User", false); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	adminToken, err := sessMgr.Create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("session Create admin: %v", err)
	}
	userToken, err := sessMgr.Create(t.Context(), "user")
	if err != nil {
		t.Fatalf("session Create user: %v", err)
	}

	return &authEnv{
		cfg: ServerConfig{
			Storage:    storage.InitBtrFSMock(),
			Systemd:    systemd.InitMockManager(),
			AccountMgr: mgr,
			SessionMgr: sessMgr,
			AuditMgr:   auditMgr,
		},
		adminToken: adminToken,
		userToken:  userToken,
	}
}

// serveFrom dispatches a request against a freshly built router with a chosen
// source address, bypassing the loopback pass that httptest would otherwise
// take.
func serveFrom(t *testing.T, cfg ServerConfig, method, path, token, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ts := &TestServer{}
	ts.ServerConfig = cfg
	handler := configureRouter(ctx, ts)

	req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader("{}"))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// serveWithHeaders dispatches a GET /status/ping against a freshly built router
// with a chosen Host and request headers, for the CORS and Private Network
// Access assertions — which are about response headers rather than status.
func serveWithHeaders(t *testing.T, cfg ServerConfig, host string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ts := &TestServer{}
	ts.ServerConfig = cfg
	handler := configureRouter(ctx, ts)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/status/ping", nil)
	req.Host = host
	req.RemoteAddr = "203.0.113.9:44100"
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// --- Repository management is administrative ---

func TestRepositoryMutationsRequireAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", authTestPassword, "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	cases := []struct{ path, body string }{
		{"repository/add", `{"name":"evil","url":"https://evil.example/repo.git"}`},
		{"repository/remove", `{"name":"default"}`},
		{"repository/refresh", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			code, body := postStatus(t, alice, tc.path, tc.body)
			if code != http.StatusForbidden {
				t.Fatalf("non-admin POST /%s = %d (%s), want 403", tc.path, code, body)
			}
		})
	}
}

// Listing stays open: which repositories are configured grants nothing, and the
// packages screen renders it for everybody.
func TestRepositoryListStillReadableByNonAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", authTestPassword, "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, alice.route("repository"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+alice.Token)
	resp, err := alice.HTTP.Do(req)
	if err != nil {
		t.Fatalf("GET /repository: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /repository = %d (%s), want 200", resp.StatusCode, body)
	}
}

// --- Saved package answers are credentials ---

func TestPackageResponsesRequireAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", authTestPassword, "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	for _, path := range []string{"packages/responses", "packages/last-responses"} {
		t.Run(path, func(t *testing.T) {
			code, body := postStatus(t, alice, path, `{"repo":"core","name":"gitea","version":"1.0"}`)
			if code != http.StatusForbidden {
				t.Fatalf("non-admin POST /%s = %d (%s), want 403", path, code, body)
			}
		})
	}
}

// --- The journal and the audit log ---

func TestJournalRoutesRequireAdminFromNonLocalhost(t *testing.T) {
	env := newAuthEnv(t)

	paths := []string{
		"/systemd/logs?unit=__system__",
		"/systemd/logs/tail?unit=town-os-package--core-gitea-1.0.service",
		"/systemd/logs/tree?repo=core&name=gitea&version=1.0",
		"/systemd/logs/tree/tail?repo=core&name=gitea&version=1.0",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := serveFrom(t, env.cfg, http.MethodGet, path, env.userToken, "203.0.113.7:44001")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("non-admin GET %s = %d (%s), want 403", path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAuditLogRequiresAdminFromNonLocalhost(t *testing.T) {
	env := newAuthEnv(t)

	rec := serveFrom(t, env.cfg, http.MethodPost, "/audit/log", env.userToken, "203.0.113.7:44002")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin POST /audit/log = %d (%s), want 403", rec.Code, rec.Body.String())
	}
}

// Localhost keeps its unauthenticated pass — it is how the controller's own
// tooling reads these, and reaching loopback already means being on the box.
func TestJournalAndAuditStillOpenToLocalhost(t *testing.T) {
	env := newAuthEnv(t)

	if rec := serveFrom(t, env.cfg, http.MethodPost, "/audit/log", "", "127.0.0.1:44003"); rec.Code == http.StatusForbidden {
		t.Fatalf("localhost POST /audit/log = 403 (%s), want it to pass", rec.Body.String())
	}
}

func TestJournalRoutesAllowAdmin(t *testing.T) {
	env := newAuthEnv(t)

	rec := serveFrom(t, env.cfg, http.MethodPost, "/audit/log", env.adminToken, "203.0.113.7:44004")
	if rec.Code == http.StatusForbidden {
		t.Fatalf("admin POST /audit/log = 403 (%s), want it to pass", rec.Body.String())
	}
}
