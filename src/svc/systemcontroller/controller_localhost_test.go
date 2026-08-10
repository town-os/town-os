// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestIsLocalhostIPv4Loopback(t *testing.T) {
	r := &http.Request{RemoteAddr: "127.0.0.1:12345"}
	if !isLocalhost(r) {
		t.Fatal("expected 127.0.0.1 to be localhost")
	}
}

func TestIsLocalhostIPv6Loopback(t *testing.T) {
	r := &http.Request{RemoteAddr: "[::1]:12345"}
	if !isLocalhost(r) {
		t.Fatal("expected ::1 to be localhost")
	}
}

func TestIsLocalhostExternalIP(t *testing.T) {
	r := &http.Request{RemoteAddr: "192.168.1.100:12345"}
	if isLocalhost(r) {
		t.Fatal("expected 192.168.1.100 to NOT be localhost")
	}
}

func TestIsLocalhostNoPort(t *testing.T) {
	r := &http.Request{RemoteAddr: "127.0.0.1"}
	if !isLocalhost(r) {
		t.Fatal("expected 127.0.0.1 (no port) to be localhost")
	}
}

func TestIsLocalhostEmptyAddr(t *testing.T) {
	r := &http.Request{RemoteAddr: ""}
	if isLocalhost(r) {
		t.Fatal("expected empty RemoteAddr to NOT be localhost")
	}
}

// initAuthTestServer creates a test server WITH session auth enabled.
// Returns the server URL and a valid admin token.
func initAuthTestServer(t *testing.T) (string, string) { //nolint:unparam // token is available for future authenticated tests
	t.Helper()
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
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, signingKey)
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := InitTestServer(ServerConfig{
		Storage:    mock,
		AccountMgr: mgr,
		SessionMgr: sessMgr,
		Systemd:    sd,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "admin", "password1", "a@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	return ts.Server.URL, resp.Token
}

func TestSystemdUnitsLocalhostNoAuth(t *testing.T) {
	serverURL, _ := initAuthTestServer(t)

	// Request without auth token — should succeed from localhost.
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, serverURL+"/systemd/units", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for localhost without auth, got %d", resp.StatusCode)
	}
}

func TestSystemdLogsTailLocalhostNoAuth(t *testing.T) {
	serverURL, _ := initAuthTestServer(t)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, serverURL+"/systemd/logs/tail", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for localhost without auth, got %d", resp.StatusCode)
	}
}

func TestSystemServicesLocalhostNoAuth(t *testing.T) {
	serverURL, _ := initAuthTestServer(t)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, serverURL+"/system-services", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for localhost without auth, got %d", resp.StatusCode)
	}
}

func TestPingLocalhostReturnsFullResponse(t *testing.T) {
	serverURL, _ := initAuthTestServer(t)

	// Unauthenticated ping from localhost should return the full response
	// (not the minimal response).
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, serverURL+"/status/ping", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var ping PingResponse
	if err := json.NewDecoder(resp.Body).Decode(&ping); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Full response includes accounts count (1 admin was created).
	if ping.Accounts != 1 {
		t.Fatalf("expected accounts=1 in full ping, got %d (might be minimal response)", ping.Accounts)
	}
}

func TestPingLocalhostIncludesLocale(t *testing.T) {
	serverURL, _ := initAuthTestServer(t)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, serverURL+"/status/ping", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	var ping PingResponse
	if err := json.NewDecoder(resp.Body).Decode(&ping); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Locale field is only in the full response (default "en-US").
	if ping.Locale != "en-US" {
		t.Fatalf("expected locale=en-US in full ping, got %q", ping.Locale)
	}
}
