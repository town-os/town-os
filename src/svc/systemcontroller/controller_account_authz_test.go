// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// POST /account/update is requireAuth, because an ordinary account has to be
// able to change its own password without an administrator. The username it
// edits comes from the request body, so the route's own middleware cannot be
// what confines it — the handler has to check that the caller is either an
// administrator or the account being edited.
//
// Without that check the escalation is one request: any authenticated account
// sets the administrator's password and logs in as it, which on a box whose
// controller drives the host podman socket is root.

// authTestPassword is the password these tests give every account they create.
const authTestPassword = "password1"

// authAs authenticates a second account and returns a client bound to its
// token, sharing the base client's transport and base URL.
func authAs(t *testing.T, c *SystemdClient, username string) *SystemdClient {
	t.Helper()
	resp, err := c.Authenticate(context.TODO(), username, authTestPassword)
	if err != nil {
		t.Fatalf("Authenticate %s: %v", username, err)
	}
	other := *c
	other.Token = resp.Token
	return &other
}

// postStatus issues a POST with the client's token and returns the status code
// and body, so a test can assert the refusal is a 403 rather than merely "an
// error" — a 500 from somewhere further in would satisfy the weaker assertion
// while leaving the escalation open.
func postStatus(t *testing.T, c *SystemdClient, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.route(path), strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
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

func TestUpdateAccountRejectsPasswordChangeOnAnotherAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	code, body := postStatus(t, alice, "account/update",
		`{"username":"testadmin","fields":{"password":"takeover1"}}`)
	if code != http.StatusForbidden {
		t.Fatalf("alice updating testadmin = %d (%s), want 403", code, body)
	}

	// The escalation is only actually prevented if the password did not change:
	// the old one must still work and the attacker's must not.
	if _, err := c.Authenticate(context.TODO(), "testadmin", "adminpass"); err != nil {
		t.Fatalf("admin password changed under us: %v", err)
	}
	if _, err := c.Authenticate(context.TODO(), "testadmin", "takeover1"); err == nil {
		t.Fatal("admin authenticated with the password alice tried to set")
	}
}

func TestUpdateAccountRejectsEditingAnotherNonAdminAccount(t *testing.T) {
	c, _ := initAccountTestClient(t)

	for _, name := range []string{"alice", "bob"} {
		if _, err := c.CreateAccount(context.TODO(), name, "password1", name+"@b.com", "555", name, false); err != nil {
			t.Fatalf("CreateAccount %s: %v", name, err)
		}
	}
	alice := authAs(t, c, "alice")

	code, body := postStatus(t, alice, "account/update",
		`{"username":"bob","fields":{"email":"attacker@example.com"}}`)
	if code != http.StatusForbidden {
		t.Fatalf("alice updating bob = %d (%s), want 403", code, body)
	}

	bob, err := c.GetAccount(context.TODO(), "bob")
	if err != nil {
		t.Fatalf("GetAccount bob: %v", err)
	}
	if bob.Email != "bob@b.com" {
		t.Fatalf("bob email = %q, want unchanged", bob.Email)
	}
}

func TestUpdateAccountAllowsSelfService(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	newpw := "password2"
	if _, err := alice.UpdateAccount(context.TODO(), "alice", account.UpdateFields{Password: &newpw}); err != nil {
		t.Fatalf("alice updating herself: %v", err)
	}
	if _, err := c.Authenticate(context.TODO(), "alice", newpw); err != nil {
		t.Fatalf("Authenticate with alice's new password: %v", err)
	}
}

func TestUpdateAccountAdminMayEditOthers(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	newpw := "resetpass1"
	// c already carries the bootstrapped admin's token.
	if _, err := c.UpdateAccount(context.TODO(), "alice", account.UpdateFields{Password: &newpw}); err != nil {
		t.Fatalf("admin updating alice: %v", err)
	}
	if _, err := c.Authenticate(context.TODO(), "alice", newpw); err != nil {
		t.Fatalf("Authenticate alice after admin reset: %v", err)
	}
}

// A non-admin may edit itself, but not every field of itself: granting is an
// administrator's decision, so self-service must not become the way around it.
func TestUpdateAccountSelfCannotGrantItself(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	alice := authAs(t, c, "alice")

	code, body := postStatus(t, alice, "account/update",
		`{"username":"alice","fields":{"grants":["gfeh"],"networks":["home"]}}`)
	if code != http.StatusForbidden {
		t.Fatalf("alice granting herself gfeh = %d (%s), want 403", code, body)
	}

	stored, err := c.GetAccount(context.TODO(), "alice")
	if err != nil {
		t.Fatalf("GetAccount alice: %v", err)
	}
	if stored.HasGrant(account.GrantGfeh) {
		t.Fatalf("alice holds %v, want no grants", stored.Grants)
	}
}

func TestUpdateAccountRequiresAToken(t *testing.T) {
	c, _ := initAccountTestClient(t)

	if _, err := c.CreateAccount(context.TODO(), "alice", "password1", "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}

	anon := *c
	anon.Token = ""
	code, body := postStatus(t, &anon, "account/update",
		`{"username":"alice","fields":{"password":"takeover1"}}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated update = %d (%s), want 401", code, body)
	}
}
