// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/http"
	"testing"
)

// Disabling an account has to end the access it already has, not just stop it
// signing in again.
//
// Authenticate is the only place that used to consult Disabled, and every
// request after a successful login is authorized from SessionManager.Validate
// alone. So a token issued before the disable kept working — and since each use
// refreshes last_used, it never idled out either. An administrator who disables
// a compromised admin account and watches it keep answering has no way to stop
// it short of restarting the controller (which rotates the signing key).

func TestDisabledAccountTokenIsRejected(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	// The token works before the disable.
	if _, err := alice.SessionUsername(context.TODO(), alice.Token); err != nil {
		t.Fatalf("SessionUsername before disable: %v", err)
	}

	if err := c.DisableAccount(context.TODO(), "alice"); err != nil {
		t.Fatalf("DisableAccount: %v", err)
	}

	// A requireAuth route.
	code, body := postStatus(t, alice, "storage", `{}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("disabled account POST /storage = %d (%s), want 401", code, body)
	}

	// And a handler that validates the token itself rather than through
	// middleware, since those are a separate code path.
	if _, err := alice.SessionUsername(context.TODO(), alice.Token); err == nil {
		t.Fatal("GET /account/me accepted a disabled account's token")
	}
}

func TestDisabledAdminTokenIsRejectedOnAdminRoutes(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "admin2", "password1", "a2@b.com", "555", "Admin2", true); err != nil {
		t.Fatalf("CreateAccount admin2: %v", err)
	}
	admin2 := authAs(t, c, "admin2")

	if err := c.DisableAccount(context.TODO(), "admin2"); err != nil {
		t.Fatalf("DisableAccount: %v", err)
	}

	// requireAdmin, and the payload would create an account if it were let
	// through — so this asserts the disabled admin cannot mint itself a way
	// back in.
	code, body := postStatus(t, admin2, "account/create",
		`{"username":"backdoor","password":"password1","email":"b@b.com","phone":"555","real_name":"B","admin":true}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("disabled admin POST /account/create = %d (%s), want 401", code, body)
	}

	if _, err := c.GetAccount(context.TODO(), "backdoor"); err == nil {
		t.Fatal("disabled admin created an account")
	}
}

// Re-enabling must not resurrect the tokens the account held when it was
// disabled: an administrator who disables an account has revoked its access,
// and enabling it later means "you may sign in again", not "your old session
// is back".
func TestDisableRevokesSessionsSoEnableDoesNotRestoreThem(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")
	oldToken := alice.Token

	if err := c.DisableAccount(context.TODO(), "alice"); err != nil {
		t.Fatalf("DisableAccount: %v", err)
	}
	if err := c.EnableAccount(context.TODO(), "alice"); err != nil {
		t.Fatalf("EnableAccount: %v", err)
	}

	revived := *c
	revived.Token = oldToken
	code, body := postStatus(t, &revived, "storage", `{}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("pre-disable token after re-enable = %d (%s), want 401", code, body)
	}

	// A fresh login still works, which is the whole point of enabling.
	if _, err := c.Authenticate(context.TODO(), "alice", "password1"); err != nil {
		t.Fatalf("Authenticate after re-enable: %v", err)
	}
}
