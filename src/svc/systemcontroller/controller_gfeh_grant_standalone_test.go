// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// The gfeh grant has to stand on its own.
//
// `wireguard` and `gfeh` are independent toggles, and neither is a prerequisite
// for the other — but "independent" has to mean an account holding only `gfeh`
// can actually sign in and use the box, not merely that the /gfeh routes accept
// its token. The account exists to run the object-storage screen in a browser,
// and a browser needs more than those routes.
//
// TestGfehGrantDoesNotBuyPeerEnrollment (controller_object_storage_test.go)
// covers the other direction: holding it must not smuggle in peer enrollment.

// TestGfehGrantAloneKeepsThePublicPing is the bug.
//
// /status/ping is registered with no auth middleware at all, so an anonymous
// stranger gets a 200. It was missing from the grant allowlist, which is a
// global fail-closed gate, so presenting a valid gfeh token turned that 200
// into a 403 — authentication making a caller strictly worse off than
// presenting nothing. The dashboard polls this every 60 seconds as its session
// heartbeat and renders its whole status surface from the reply, so the account
// could reach every object-storage route and still not get a usable page.
func TestGfehGrantAloneKeepsThePublicPing(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantGfeh}, []string{"office"})

	code, out := e.do(t, http.MethodGet, "status/ping", token, "")
	if code != http.StatusOK {
		t.Fatalf("gfeh-only account GET /status/ping = %d (%s), want 200", code, out)
	}
	// And it is the authenticated reply, not the minimal one a stranger gets:
	// the UI logs the session out when the username is missing.
	if !strings.Contains(out, `"username":"`+scopedAccountName+`"`) {
		t.Errorf("ping did not identify the caller, so the UI would treat the session as dead: %s", out)
	}
}

// A token must never buy less than no token at all. Pinned separately because
// the failure is invisible from the authenticated side alone — a 403 there
// looks like an ordinary permissions decision until you notice the same request
// succeeds unauthenticated.
func TestGfehGrantAloneIsNotWorseOffThanAnonymous(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantGfeh}, []string{"office"})

	anonCode, _ := e.do(t, http.MethodGet, "status/ping", "", "")
	grantCode, out := e.do(t, http.MethodGet, "status/ping", token, "")

	if anonCode != http.StatusOK {
		t.Fatalf("anonymous GET /status/ping = %d, want 200 (it is a public route)", anonCode)
	}
	if grantCode != anonCode {
		t.Errorf("GET /status/ping = %d with a gfeh token but %d with none (%s): "+
			"authenticating made the caller worse off", grantCode, anonCode, out)
	}
}

// Signing in, finding out who you are, discovering your networks, and trusting
// the box. Without these the grant is unusable no matter what /gfeh admits,
// which is why they are common to every grant rather than duplicated into each.
func TestGfehGrantAloneReachesTheCommonRoutes(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantGfeh}, []string{"office"})

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "account/me"},
		{http.MethodGet, "networks"},
		{http.MethodGet, "dns/services"},
		{http.MethodGet, "tls/ca.crt"},
	} {
		if code, out := e.do(t, r.method, r.path, token, ""); code == http.StatusForbidden {
			t.Errorf("gfeh-only account %s /%s = 403 (%s): the grant cannot be exercised without it",
				r.method, r.path, out)
		}
	}
}

// Every route the grant is declared to unlock, unlocked by the grant alone.
// Driven off grantRoutes itself rather than a hand-copied list, so a gfeh route
// added later is covered here the moment it is declared — the allowlist is
// fail-closed, and this is the half that proves it is not closed too far.
func TestGfehGrantAloneReachesEveryDeclaredGfehRoute(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantGfeh}, []string{"office"})

	// Bodies for the mutating routes. A route with no entry is a GET.
	bodies := map[string]string{
		"POST /gfeh/principals/add":     `{"network":"office","principal":"someone"}`,
		"POST /gfeh/principals/remove":  `{"network":"office","principal":"someone"}`,
		"POST /gfeh/grants/add":         `{"network":"office","principal":"someone","path":"/","perm":["read"]}`,
		"POST /gfeh/grants/revoke":      `{"network":"office","id":1}`,
		"POST /gfeh/exposures/withdraw": `{"network":"office","token":"abc"}`,
	}
	// The reads name their network in the query, and requireNetworkScope
	// confines them to it.
	queries := map[string]string{
		"GET /gfeh/principals": "?network=office",
		"GET /gfeh/grants":     "?network=office&principal=someone",
		"GET /gfeh/exposures":  "?network=office",
	}

	seen := 0
	for route, grant := range grantRoutes {
		if grant != account.GrantGfeh {
			continue
		}
		seen++
		method, path, ok := strings.Cut(route, " ")
		if !ok {
			t.Fatalf("malformed grantRoutes key %q", route)
		}
		// Whatever the handler then makes of it — 404 for a partition that does
		// not exist, 503 for a daemon that is not answering — is not this
		// test's business. 403 is: it would mean the grant did not admit its
		// own declared route.
		code, out := e.do(t, method, strings.TrimPrefix(path, "/")+queries[route], token, bodies[route])
		if code == http.StatusForbidden {
			t.Errorf("gfeh-only account %s = 403 (%s), but grantRoutes declares it unlocked by %q",
				route, out, grant)
		}
	}
	if seen == 0 {
		t.Fatal("no gfeh routes found in grantRoutes; this test would pass vacuously")
	}
}

// The converse, so the allowlist is not merely open: a gfeh grant is not a
// wireguard grant, and it does not reach a route no grant unlocks.
func TestGfehGrantAloneStillStopsAtUngrantedRoutes(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantGfeh}, []string{"office"})

	for _, r := range []struct{ method, path, body string }{
		// Provisioning a partition creates the root of a permission tree.
		{http.MethodPost, "gfeh/partitions/create", `{"name":"office"}`},
		// Aggregates every account's peers across every network.
		{http.MethodGet, "networks/peers/connected", ""},
		// Nothing to do with object storage at all.
		{http.MethodGet, "settings", ""},
	} {
		if code, out := e.do(t, r.method, r.path, token, r.body); code != http.StatusForbidden {
			t.Errorf("gfeh-only account %s /%s = %d (%s), want 403", r.method, r.path, code, out)
		}
	}
}
