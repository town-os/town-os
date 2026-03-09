// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"
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
