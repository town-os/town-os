// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// End-to-end cover for the routes that were reachable by any account with a
// login: package repositories (a code-supply channel), saved package answers
// (credentials), and the audit log.
//
// The journal endpoints are covered by the unit tests instead. They are
// localhost-gated, and everything here connects over loopback — isLocalhost
// passes those requests through before the admin check is reached, so a test
// server cannot exercise the branch a LAN client takes. The unit test drives
// the router directly with a non-loopback source address.

func TestSystemControllerRepositoryMutationsRequireAdmin(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	cases := []struct{ path, body string }{
		{"repository/add", `{"name":"evil","url":"https://evil.example/repo.git"}`},
		{"repository/remove", `{"name":"default"}`},
		{"repository/refresh", `{}`},
	}
	for _, tc := range cases {
		code, body := e.post(t, tc.path, e.userToken, tc.body)
		if code != http.StatusForbidden {
			t.Errorf("non-admin POST /%s = %d (%s), want 403", tc.path, code, body)
		}
	}
}

func TestSystemControllerPackageResponsesRequireAdmin(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	for _, path := range []string{"packages/responses", "packages/last-responses"} {
		code, body := e.post(t, path, e.userToken, `{"repo":"core","name":"gitea","version":"1.0"}`)
		if code != http.StatusForbidden {
			t.Errorf("non-admin POST /%s = %d (%s), want 403", path, code, body)
		}
	}
}

// The login throttle is a real limit on a public endpoint, so it is worth
// confirming over the wire rather than only against the limiter type. A
// nonexistent username costs no password hash, so this does not pay 21 × 64 MiB
// to prove it.
func TestSystemControllerLoginRateLimited(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	body := `{"username":"nobody","password":"whatever1"}`
	var last int
	for range 40 {
		code, _ := e.post(t, "account/authenticate", "", body)
		last = code
		if code == http.StatusTooManyRequests {
			break
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("unexpected status %d while filling the login window", code)
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("login attempts were never throttled (last status %d)", last)
	}

	// A correct password is refused too while the window is full: the limit is
	// on the source, not on the guess, or an attacker would learn which
	// usernames exist by which ones were still being checked.
	code, _ := e.post(t, "account/authenticate", "",
		`{"username":"admin","password":"`+authzAdminPassword+`"}`)
	if code == http.StatusOK {
		t.Fatal("a valid login succeeded while the source was rate-limited")
	}
}

// The audit log is admin-only now. Everything here is loopback, so the
// localhost pass applies — this asserts the pass still works, which is what the
// controller's own tooling depends on.
func TestSystemControllerAuditLogStillReadableOverLoopback(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	code, body := e.post(t, "audit/log", e.userToken, `{}`)
	if code != http.StatusOK {
		t.Fatalf("loopback POST /audit/log = %d (%s), want 200", code, body)
	}
}

// Whatever the audit log contains, it must not contain the answers to a
// package's questions: a `type: secret` answer is a generated credential and a
// `type: oauth` one is a vendor token.
func TestSystemControllerAuditDetailOmitsInstallResponses(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	const secret = "sup3rs3cr3tpassw0rd"

	// An audited action carrying a credential in a nested field. The old
	// sanitizer deleted a top-level key named "password" and nothing else, so
	// this shape — {"fields":{"password":…}} — went into the log verbatim.
	if code, body := e.post(t, "account/update", e.adminToken,
		`{"username":"user","fields":{"password":"`+secret+`"}}`); code != http.StatusOK {
		t.Fatalf("account/update = %d (%s), want 200", code, body)
	}

	code, body := e.post(t, "audit/log", e.adminToken, `{}`)
	if code != http.StatusOK {
		t.Fatalf("POST /audit/log = %d (%s), want 200", code, body)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("install answer was written to the audit log: %s", body)
	}
}

// A sanity check that the tightening did not take the login page with it.
func TestSystemControllerAuthenticateStillPublic(t *testing.T) {
	t.Parallel()
	e := initAuthzTest(t)

	if _, err := e.client.Authenticate(context.TODO(), "user", authzUserPassword); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}
