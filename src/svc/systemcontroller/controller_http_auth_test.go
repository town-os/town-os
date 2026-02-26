package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPAuthenticate(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password123", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "alice", "password123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if resp.Account.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", resp.Account.Username)
	}
}

func TestHTTPAuthenticateWrongPassword(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password123", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	_, err := c.Authenticate(context.TODO(), "alice", "wrongpass")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestHTTPSessionLifecycle(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "alice", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// list sessions using the token
	sessions, err := c.ListSessions(context.TODO(), resp.Token)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// get username
	username, err := c.SessionUsername(context.TODO(), resp.Token)
	if err != nil {
		t.Fatalf("SessionUsername: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", username)
	}

	// revoke session
	if err := c.RevokeSession(context.TODO(), sessions[0].ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	// token should no longer work for listing sessions
	_, err = c.ListSessions(context.TODO(), resp.Token)
	if err == nil {
		t.Fatal("expected error after session revoke")
	}
}

func TestHTTPSessionUsernameUnauthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, c.BaseURL+"/account/me", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("GET /account/me: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("body close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestHTTPSessionUsernameAuthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	authResp, err := c.Authenticate(context.TODO(), "alice", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	username, err := c.SessionUsername(context.TODO(), authResp.Token)
	if err != nil {
		t.Fatalf("SessionUsername: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", username)
	}
}

// --- Admin middleware tests ---

func TestAdminMiddlewareBlocksNonAdmin(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// try to install a package (admin-only)
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("packages/install"), bytes.NewBufferString(`{"name":"test","version":"1.0"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", httpResp.StatusCode)
	}
}

func TestAdminMiddlewareAllowsAdmin(t *testing.T) {
	db, err := account.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "repo-a", URL: *u}}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		AccountMgr:     mgr,
		SessionMgr:     sessMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// create admin user
	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// admin should be able to install
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("packages/install"), bytes.NewBufferString(`{"name":"nginx","version":"1.0","responses":{}}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", httpResp.StatusCode)
	}
}

func TestAdminMiddlewareNoToken(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// try without any token
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("packages/install"), bytes.NewBufferString(`{"name":"test","version":"1.0"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", httpResp.StatusCode)
	}
}

// --- requireAuth middleware tests ---

func TestHTTPRequireAuthBlocksUnauthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// try accessing a protected route without a token
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route("storage/create"), bytes.NewBufferString(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", httpResp.StatusCode)
	}
}

func TestHTTPRequireAuthAllowsAuthenticated(t *testing.T) {
	c, _ := initAccountTestClient(t)

	// create non-admin user and authenticate
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// access a non-admin protected route (list accounts)
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, c.route("account"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.Token)

	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for authenticated non-admin, got %d", httpResp.StatusCode)
	}
}
