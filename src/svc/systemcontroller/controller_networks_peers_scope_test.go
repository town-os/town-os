// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// GET /networks/peers is on the wireguard grant's allowlist, so a scoped
// account reaches it — and it named the network in a query parameter that
// nothing checked against the caller's scope. peers/add and peers/refresh both
// confine; the read did not, which is the easier one to forget and the one that
// leaks: a peer list is device names, the accounts that enrolled them, their
// overlay addresses, and their public keys.

func TestListPeersOutOfScopeDenied(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	token := e.authToken(t, "portal", "portalpass1")

	// Seed a peer on the out-of-scope network so a leak would have content.
	if code, body := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken, `{"network":"lab","name":"admin-laptop","public_key":"LABKEY"}`); code != http.StatusOK {
		t.Fatalf("admin peers/add lab = %d (%s), want 200", code, body)
	}

	code, body := e.do(t, http.MethodGet, "networks/peers?network=lab", token, "")
	if code != http.StatusForbidden {
		t.Fatalf("GET /networks/peers?network=lab = %d (%s), want 403", code, body)
	}
	if strings.Contains(body, "LABKEY") || strings.Contains(body, "admin-laptop") {
		t.Fatalf("out-of-scope peer leaked in the refusal body: %s", body)
	}
}

func TestListPeersInScopeAllowed(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	token := e.authToken(t, "portal", "portalpass1")

	if code, body := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"office","name":"phone","public_key":"OFFICEKEY"}`); code != http.StatusOK {
		t.Fatalf("peers/add office = %d (%s), want 200", code, body)
	}

	code, body := e.do(t, http.MethodGet, "networks/peers?network=office", token, "")
	if code != http.StatusOK {
		t.Fatalf("GET /networks/peers?network=office = %d (%s), want 200", code, body)
	}
	if !strings.Contains(body, "OFFICEKEY") {
		t.Fatalf("in-scope peer list did not include the enrolled peer: %s", body)
	}
}

// An ordinary account holds no grants and therefore no scope, and an empty
// scope denies every network. Confining it the way a grant-holder is confined
// would 403 a route that is deliberately requireAuth, so it must stay readable.
func TestListPeersUnscopedAccountUnaffected(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateAccount(context.Background(), "plain", "plainpass1", "u@test.com", "555-2", "Plain", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	token := e.authToken(t, "plain", "plainpass1")

	if code, body := e.do(t, http.MethodGet, "networks/peers?network=lab", token, ""); code != http.StatusOK {
		t.Fatalf("plain account GET /networks/peers = %d (%s), want 200", code, body)
	}
}
