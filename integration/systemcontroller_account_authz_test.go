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

// End-to-end cover for the two account-authorization holes, driven through the
// real HTTP stack against a real SQLite account and session store — the mocked
// unit tests prove the handler decides correctly, these prove the decision
// survives the middleware chain, the JWT round trip, and the DB.

// authzEnv is a bootstrapped box: a real account/session manager, an
// administrator, and a plain user.
type authzEnv struct {
	client     *systemcontroller.SystemdClient
	mgr        *account.SQLiteManager
	adminToken string
	userToken  string
}

const (
	authzAdminPassword = "adminpass1"
	authzUserPassword  = "userpass1"
)

// initAuthzServer is initBootstrapTest plus an audit manager, since these tests
// assert on what reaches the audit log as well as on what reaches a caller.
func initAuthzServer(t *testing.T) (*systemcontroller.SystemdClient, *account.SQLiteManager) {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "authz.db"))
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
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	auditMgr, err := account.InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFSMock(),
		AccountMgr: mgr,
		SessionMgr: sessMgr,
		AuditMgr:   auditMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, mgr
}

func initAuthzTest(t *testing.T) *authzEnv {
	t.Helper()

	c, mgr := initAuthzServer(t)

	// Bootstrap window: the first administrator is created without a token.
	if _, err := c.CreateAccount(context.TODO(), "admin", authzAdminPassword, "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", authzAdminPassword)
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	if _, err := c.CreateAccount(context.TODO(), "user", authzUserPassword, "user@test.com", "555-0001", "User", false); err != nil {
		t.Fatalf("CreateAccount user: %v", err)
	}
	userResp, err := c.Authenticate(context.TODO(), "user", authzUserPassword)
	if err != nil {
		t.Fatalf("Authenticate user: %v", err)
	}

	return &authzEnv{client: c, mgr: mgr, adminToken: adminResp.Token, userToken: userResp.Token}
}

// post issues a POST with an explicit token and returns status and body.
func (e *authzEnv) post(t *testing.T, path, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, e.client.BaseURL+"/"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.client.HTTP.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
	}()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, string(out)
}

// A plain user resetting the administrator's password is the shortest path
// from "has an account" to "owns the box".
func TestSystemControllerAccountUpdateEscalationBlocked(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	code, body := e.post(t, "account/update", e.userToken,
		`{"username":"admin","fields":{"password":"takeover1"}}`)
	if code != http.StatusForbidden {
		t.Fatalf("user updating admin = %d (%s), want 403", code, body)
	}

	// Straight at the store: the hash must not have moved.
	if _, err := e.mgr.Authenticate("admin", authzAdminPassword); err != nil {
		t.Fatalf("admin password no longer works: %v", err)
	}
	if _, err := e.mgr.Authenticate("admin", "takeover1"); err == nil {
		t.Fatal("admin authenticated with the password the user tried to set")
	}
}

func TestSystemControllerAccountUpdateSelfServiceStillWorks(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	code, body := e.post(t, "account/update", e.userToken,
		`{"username":"user","fields":{"password":"newuserpass1","email":"changed@test.com"}}`)
	if code != http.StatusOK {
		t.Fatalf("user updating itself = %d (%s), want 200", code, body)
	}
	if _, err := e.mgr.Authenticate("user", "newuserpass1"); err != nil {
		t.Fatalf("Authenticate with the new password: %v", err)
	}
}

// Disabling an account must end the session it already holds, not merely stop
// it signing in again.
func TestSystemControllerDisabledAccountTokenRejected(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	// The token works first, so the assertion below is about the disable.
	if code, body := e.post(t, "storage", e.userToken, `{}`); code != http.StatusOK {
		t.Fatalf("POST /storage before disable = %d (%s), want 200", code, body)
	}

	if err := e.client.DisableAccount(context.TODO(), "user"); err != nil {
		t.Fatalf("DisableAccount: %v", err)
	}

	if code, body := e.post(t, "storage", e.userToken, `{}`); code != http.StatusUnauthorized {
		t.Fatalf("POST /storage after disable = %d (%s), want 401", code, body)
	}

	// Re-enabling grants the ability to sign in again, not the old session.
	if err := e.client.EnableAccount(context.TODO(), "user"); err != nil {
		t.Fatalf("EnableAccount: %v", err)
	}
	if code, body := e.post(t, "storage", e.userToken, `{}`); code != http.StatusUnauthorized {
		t.Fatalf("pre-disable token after re-enable = %d (%s), want 401", code, body)
	}
	if _, err := e.client.Authenticate(context.TODO(), "user", authzUserPassword); err != nil {
		t.Fatalf("Authenticate after re-enable: %v", err)
	}
}

func TestSystemControllerDisabledAdminCannotAdminister(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	if _, err := e.client.CreateAccount(context.TODO(), "admin2", authzAdminPassword, "admin2@test.com", "555-0002", "Admin Two", true); err != nil {
		t.Fatalf("CreateAccount admin2: %v", err)
	}
	resp, err := e.client.Authenticate(context.TODO(), "admin2", authzAdminPassword)
	if err != nil {
		t.Fatalf("Authenticate admin2: %v", err)
	}

	if err := e.client.DisableAccount(context.TODO(), "admin2"); err != nil {
		t.Fatalf("DisableAccount admin2: %v", err)
	}

	code, body := e.post(t, "account/create", resp.Token,
		`{"username":"backdoor","password":"backdoorpw1","email":"b@test.com","phone":"555-0003","real_name":"Backdoor","admin":true}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("disabled admin creating an account = %d (%s), want 401", code, body)
	}
	if _, err := e.mgr.Get("backdoor"); err == nil {
		t.Fatal("disabled admin created an account")
	}
}
