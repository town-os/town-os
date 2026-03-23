// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- Bootstrap account creation integration tests ---

func TestBootstrapCreateAccountNoUsers(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// No accounts exist — should succeed without any token.
	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@test.com", "555-0001", "First Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount on empty DB: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
	if !acct.Admin {
		t.Fatal("expected admin=true for bootstrap account")
	}
}

func TestBootstrapCreateAccountWithStaleToken(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Set a stale/invalid token — bootstrap mode should ignore it.
	c.Token = "stale-garbage-token-from-previous-session"

	acct, err := c.CreateAccount(context.TODO(), "first", "password1", "f@test.com", "555-0001", "First Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with stale token on empty DB: %v", err)
	}
	if acct.Username != "first" {
		t.Fatalf("expected username %q, got %q", "first", acct.Username)
	}
}

func TestBootstrapCreateAccountAllAdminsDisabled(t *testing.T) {
	c, mgr := initBootstrapTest(t)

	// Create an admin and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Disable the only admin.
	if err := mgr.Disable("admin"); err != nil {
		t.Fatalf("Disable admin: %v", err)
	}

	// Clear token — bootstrap mode should re-engage.
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount after all admins disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapCreateAccountAllAdminsDisabledWithStaleToken(t *testing.T) {
	c, mgr := initBootstrapTest(t)

	// Create an admin and authenticate.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Disable the only admin.
	if err := mgr.Disable("admin"); err != nil {
		t.Fatalf("Disable admin: %v", err)
	}

	// Keep the (now-stale) token — bootstrap mode should ignore it.
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with stale token after all admins disabled: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapCreateAccountOnlyNonAdminUsers(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Create a non-admin user.
	if _, err := c.CreateAccount(context.TODO(), "regular", "password1", "r@test.com", "555-0001", "Regular User", false); err != nil {
		t.Fatalf("CreateAccount regular: %v", err)
	}

	// Disable the only admin.
	if err := c.DisableAccount(context.TODO(), "admin"); err != nil {
		t.Fatalf("DisableAccount admin: %v", err)
	}

	// No enabled admins remain (only a non-admin) — bootstrap should allow create.
	c.Token = ""
	acct, err := c.CreateAccount(context.TODO(), "newadmin", "password1", "new@test.com", "555-0002", "New Admin", true)
	if err != nil {
		t.Fatalf("bootstrap CreateAccount with only non-admin users: %v", err)
	}
	if acct.Username != "newadmin" {
		t.Fatalf("expected username %q, got %q", "newadmin", acct.Username)
	}
}

func TestBootstrapRejectsAfterAdminExists(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// Bootstrap first admin.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Unauthenticated create should be rejected (admin exists).
	c.Token = ""
	_, err := c.CreateAccount(context.TODO(), "intruder", "password1", "i@test.com", "555-9999", "Intruder", false)
	if err == nil {
		t.Fatal("expected error for unauthenticated create when enabled admin exists")
	}
}

func TestBootstrapPingNeedsSetup(t *testing.T) {
	c, _ := initBootstrapTest(t)

	// No accounts — needs_setup should be true.
	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !ping.NeedsSetup {
		t.Fatal("expected needs_setup=true with no accounts")
	}

	// Create admin (no auth needed on empty DB).
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}

	// Admin exists — needs_setup should be false regardless of sessions.
	ping, err = c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ping.NeedsSetup {
		t.Fatal("expected needs_setup=false with admin account present")
	}
}

func TestBootstrapPingIncludesExternalIP(t *testing.T) {
	dir := t.TempDir()
	db, err := account.OpenDB(filepath.Join(dir, "test.db"))
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
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    mock,
		AccountMgr: mgr,
		SessionMgr: sessMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	// Inject a fake external IP.
	ts.SetExternalIP("198.51.100.7")

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap admin and authenticate to get full ping response.
	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	c.Token = resp.Token

	ping, err := c.Ping(context.TODO())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if ping.ExternalIP != "198.51.100.7" {
		t.Fatalf("expected external_ip %q, got %q", "198.51.100.7", ping.ExternalIP)
	}

	// InternalIP should be present (non-empty on any machine with a network interface).
	if ping.InternalIP == "" {
		t.Log("internal_ip is empty (expected on machines without non-loopback interfaces)")
	}
}
