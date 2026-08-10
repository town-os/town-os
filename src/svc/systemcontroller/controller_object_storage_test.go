// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
)

// Who may administer object storage, and on which network.
//
// These tests drive the real middleware chain (initWireGuardTestEnv builds a
// full HTTP server with sessions), because the whole point of the rules is
// which requests survive that chain: the global network-only allowlist, then
// the route's own requireAuth/requireObjectStorage, then the per-network scope
// check inside the handler. A unit test of any one of those three would pass
// while the composition let the wrong caller through.

// gfehMutators are the five mutating object-storage routes, with a body naming
// the `office` network.
var gfehMutators = []struct {
	path string
	body string
}{
	{"gfeh/principals/add", `{"network":"office","principal":"someone"}`},
	{"gfeh/principals/remove", `{"network":"office","principal":"someone"}`},
	{"gfeh/grants/add", `{"network":"office","principal":"someone","path":"/","perm":["read"]}`},
	{"gfeh/grants/revoke", `{"network":"office","id":1}`},
	{"gfeh/exposures/withdraw", `{"network":"office","token":"abc"}`},
}

// newPlainAccount creates an ordinary (non-admin, non-network-only) account and
// returns its token.
func newPlainAccount(t *testing.T, e *wgTestEnv, username, password string) string {
	t.Helper()
	if _, err := e.client.CreateAccount(context.Background(), username, password, username+"@test.com", "555-0001", "Test User", false); err != nil {
		t.Fatalf("CreateAccount %s: %v", username, err)
	}
	return e.authToken(t, username, password)
}

// scopedAccountName and scopedAccountPassword are the network-only account every
// test below drives. Named constants rather than per-test literals so the
// helper cannot drift from the authToken call that follows it.
const (
	scopedAccountName     = "portal"
	scopedAccountPassword = "portalpass1"
)

// newNetworkOnlyAccount creates the scoped account holding the gfeh grant.
func newNetworkOnlyAccount(t *testing.T, e *wgTestEnv, networks []string) string {
	t.Helper()
	return newGrantedAccount(t, e, []string{account.GrantGfeh}, networks)
}

// newGrantedAccount creates the scoped account holding exactly grants, and
// returns its token.
func newGrantedAccount(t *testing.T, e *wgTestEnv, grants, networks []string) string {
	t.Helper()
	if _, err := e.client.CreateGrantedAccount(context.Background(), scopedAccountName, scopedAccountPassword,
		scopedAccountName+"@test.com", "555-0002", "Scoped", grants, networks); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	return e.authToken(t, scopedAccountName, scopedAccountPassword)
}

// An administrator holds every grant, so nothing in this chain may refuse one.
// 503 is the authorized answer in this server (no gfeh registry is configured),
// so "not 403" is what distinguishes admitted from denied throughout.
func TestAdminMayAdministerObjectStorageOnEveryNetwork(t *testing.T) {
	e := initWireGuardTestEnv(t)

	for _, m := range gfehMutators {
		if code, out := e.do(t, http.MethodPost, m.path, e.adminToken, m.body); code == http.StatusForbidden {
			t.Errorf("admin POST /%s = 403 (%s), want it admitted", m.path, out)
		}
	}
	// Including a network the administrator was never scoped to, because an
	// administrator is never scoped to anything.
	body := `{"network":"a-network-nobody-mentioned","principal":"someone"}`
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", e.adminToken, body); code == http.StatusForbidden {
		t.Errorf("admin on an arbitrary network = 403 (%s), want it admitted", out)
	}
}

// A plain dashboard account administers no network. Object storage is not
// self-service: adding a principal to a partition hands out access to files.
func TestPlainAccountMayNotAdministerObjectStorage(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newPlainAccount(t, e, "bob", "bobpass12345")

	for _, m := range gfehMutators {
		if code, out := e.do(t, http.MethodPost, m.path, token, m.body); code != http.StatusForbidden {
			t.Errorf("plain account POST /%s = %d (%s), want 403", m.path, code, out)
		}
	}
}

// A network-only account administers object storage on the networks it is
// scoped to. This is the half of "network-only" that used to be a separate
// capability, and it must work without the account being an administrator.
func TestNetworkOnlyAccountMayAdministerItsOwnNetwork(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	for _, m := range gfehMutators {
		if code, out := e.do(t, http.MethodPost, m.path, token, m.body); code == http.StatusForbidden {
			t.Errorf("scoped account POST /%s on its own network = 403 (%s), want it admitted", m.path, out)
		}
	}
}

// ... and only those. A scope that did not confine would make the account kind
// meaningless: the whole difference between it and an administrator is which
// networks it reaches.
func TestNetworkOnlyAccountIsConfinedToItsScope(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	for _, m := range gfehMutators {
		body := `{"network":"home","principal":"someone","path":"/","perm":["read"],"id":1,"token":"abc"}`
		if code, out := e.do(t, http.MethodPost, m.path, token, body); code != http.StatusForbidden {
			t.Errorf("scoped account POST /%s on another network = %d (%s), want 403", m.path, code, out)
		}
	}
}

// Authorization must not depend on the state of the thing being addressed.
//
// This server has no gfeh registry, so every partition lookup fails — which is
// exactly the condition that makes the ordering visible. An out-of-scope
// request must still be refused with 403, while an in-scope one falls through
// to the 503 the missing registry earns. With the lookup first, both answered
// 503: a scoped account reaching outside its scope was told "the service is
// down", learned whether the partition existed, and produced no record of
// having reached outside its scope at all.
func TestScopeIsRefusedBeforeThePartitionIsResolved(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, addPrincipalBody("home")); code != http.StatusForbidden {
		t.Errorf("out of scope with no registry = %d (%s), want 403 before the lookup", code, out)
	}
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, addPrincipalBody("office")); code != http.StatusServiceUnavailable {
		t.Errorf("in scope with no registry = %d (%s), want the 503 the lookup earns", code, out)
	}

	// A request that names no network at all is a malformed request, not a
	// permissions problem, and must read as one for a scoped account too.
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, `{"principal":"someone"}`); code != http.StatusBadRequest {
		t.Errorf("empty network = %d (%s), want 400", code, out)
	}
}

// addPrincipalBody is the canonical mutating request, on a named network.
func addPrincipalBody(network string) string {
	return `{"network":"` + network + `","principal":"someone"}`
}

// The confinement covers reads too. A scoped account listing another network's
// principals or published links would be an information leak of exactly the
// kind the scope exists to prevent — and reads are requireAuth, so nothing
// upstream of the handler would have stopped it.
func TestNetworkOnlyAccountCannotReadAnotherNetworksPartition(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	for _, path := range []string{
		"gfeh/principals?network=home",
		"gfeh/grants?network=home&principal=someone",
		"gfeh/exposures?network=home",
	} {
		if code, out := e.do(t, http.MethodGet, path, token, ""); code != http.StatusForbidden {
			t.Errorf("scoped account GET /%s = %d (%s), want 403", path, code, out)
		}
	}
}

// Reads stay open to any authenticated account — the account list is already
// visible, so who holds which grant is no more sensitive. This pins that
// folding the capability in did not tighten them by accident.
func TestObjectStorageReadsRemainOpenToAnyAccount(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newPlainAccount(t, e, "reader", "readerpass123")

	for _, path := range []string{
		"gfeh",
		"gfeh/principals?network=office",
		"gfeh/grants?network=office&principal=someone",
		"gfeh/exposures?network=office",
	} {
		if code, out := e.do(t, http.MethodGet, path, token, ""); code == http.StatusForbidden {
			t.Errorf("GET /%s = 403 (%s), want it admitted", path, out)
		}
	}
}

// GET /gfeh names no network, so the per-request scope check cannot cover it:
// it enumerates them, and the filter lives in the handler. The two halves of
// that filter are what this pins, on a server that actually has partitions —
// with none configured the endpoint returns [] for everybody and the "reads
// stay open" test above would pass while every ordinary account saw nothing.
//
// The filter must key on the same "is this account confined at all" question
// the scope check asks. Keying it on the network scope alone reads an ordinary
// account's empty scope as "no networks", which is how a plain reader ends up
// with an empty partition list on a box that has two.
func TestGfehListShowsEveryPartitionToAPlainAccountAndOnlyTheScopeToAGrantHolder(t *testing.T) {
	e := initWireGuardTestEnvWith(t, func(cfg *ServerConfig) {
		cfg.GfehRegistry = stubGfehRegistry{clients: map[string]gfeh.Client{
			"office": allViews("office", "office"),
			"lab":    allViews("lab", "lab"),
		}}
	})

	networksFor := func(t *testing.T, who, token string) []string {
		t.Helper()
		code, out := e.do(t, http.MethodGet, "gfeh", token, "")
		if code != http.StatusOK {
			t.Fatalf("%s GET /gfeh = %d (%s), want 200", who, code, out)
		}
		var views []GfehPartitionView
		if err := json.Unmarshal([]byte(out), &views); err != nil {
			t.Fatalf("%s GET /gfeh decode %q: %v", who, out, err)
		}
		got := make([]string, 0, len(views))
		for _, v := range views {
			got = append(got, v.Network)
		}
		slices.Sort(got)
		return got
	}

	if got := networksFor(t, "admin", e.adminToken); !slices.Equal(got, []string{"lab", "office"}) {
		t.Errorf("admin sees %v, want both partitions", got)
	}

	plain := newPlainAccount(t, e, "reader", "readerpass123")
	if got := networksFor(t, "plain account", plain); !slices.Equal(got, []string{"lab", "office"}) {
		t.Errorf("plain account sees %v, want both partitions", got)
	}

	scoped := newNetworkOnlyAccount(t, e, []string{"office"})
	if got := networksFor(t, "scoped account", scoped); !slices.Equal(got, []string{"office"}) {
		t.Errorf("scoped account sees %v, want only its own network", got)
	}
}

// Provisioning a partition creates the root of a permission tree and a btrfs
// subvolume with a quota. TOWNOS_CONTRACT.md reserves that for administrators
// and gfeh's client branches on the 403, so a network-only account must NOT
// reach it — not even for a network in its own scope.
func TestNetworkOnlyAccountCannotProvisionPartitions(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	for _, p := range []struct{ path, body string }{
		{"gfeh/partitions/create", `{"name":"office","quota":0}`},
		{"gfeh/partitions/modify", `{"name":"office","quota":1}`},
		{"gfeh/partitions/remove", `{"name":"office"}`},
	} {
		if code, out := e.do(t, http.MethodPost, p.path, token, p.body); code != http.StatusForbidden {
			t.Errorf("scoped account POST /%s = %d (%s), want 403", p.path, code, out)
		}
	}
}

// /account/update is requireAuth, so without an explicit check any
// authenticated account could make itself network-only and walk into that
// network's partition and its peer enrollment. This is the escalation that
// check exists to stop.
func TestAccountCannotMakeItselfNetworkOnly(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newPlainAccount(t, e, "climber", "climberpass1")

	body := `{"username":"climber","fields":{"grants":["gfeh"],"networks":["office"]}}`
	if code, out := e.do(t, http.MethodPost, "account/update", token, body); code != http.StatusForbidden {
		t.Fatalf("self-promotion = %d (%s), want 403", code, out)
	}

	// Nothing was written: the account still cannot reach a mutator.
	add := `{"network":"office","principal":"climber"}`
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, add); code != http.StatusForbidden {
		t.Errorf("after a refused self-promotion, POST /gfeh/principals/add = %d (%s), want 403", code, out)
	}

	// A normal edit by that same account still works -- the guard is scoped to
	// the kind and its scope, not to the route.
	plain := `{"username":"climber","fields":{"phone":"555-9999"}}`
	if code, out := e.do(t, http.MethodPost, "account/update", token, plain); code != http.StatusOK {
		t.Errorf("ordinary self-edit = %d (%s), want 200", code, out)
	}
}

// Widening an existing scoped account's own scope is the same escalation in a
// smaller costume, so the guard covers a bare `networks` edit as well.
func TestNetworkOnlyAccountCannotWidenItsOwnScope(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	body := `{"username":"portal","fields":{"networks":["office","home"]}}`
	if code, out := e.do(t, http.MethodPost, "account/update", token, body); code != http.StatusForbidden {
		t.Fatalf("self-widening = %d (%s), want 403", code, out)
	}

	deny := `{"network":"home","principal":"portal"}`
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, deny); code != http.StatusForbidden {
		t.Errorf("after a refused self-widening, the account reached home: %d (%s)", code, out)
	}
}

// An administrator can widen it, and the change takes effect on the session
// that is already open: the account is read from behind the token on every
// request, so nothing has to re-authenticate.
func TestAdminCanWidenAScopeAndItTakesEffectImmediately(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	deny := `{"network":"home","principal":"portal"}`
	if code, _ := e.do(t, http.MethodPost, "gfeh/principals/add", token, deny); code != http.StatusForbidden {
		t.Fatalf("setup: scoped account already reached home (%d)", code)
	}

	widen := `{"username":"portal","fields":{"networks":["office","home"]}}`
	if code, out := e.do(t, http.MethodPost, "account/update", e.adminToken, widen); code != http.StatusOK {
		t.Fatalf("admin widening = %d (%s), want 200", code, out)
	}

	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, deny); code == http.StatusForbidden {
		t.Errorf("the widened scope did not take effect on the open session (%s)", out)
	}
}

// An account created network-only arrives able to do the job, so an
// administrator does not have to create it and then remember to edit it.
func TestCreateAccountAppliesTheNetworkOnlyKind(t *testing.T) {
	e := initWireGuardTestEnv(t)

	body := `{"username":"portal","password":"portalpass1","email":"p@test.com","phone":"555-0002","real_name":"Portal","grants":["gfeh"],"networks":["office"]}`
	if code, out := e.do(t, http.MethodPost, "account/create", e.adminToken, body); code != http.StatusOK {
		t.Fatalf("create network-only = %d (%s), want 200", code, out)
	}

	token := e.authToken(t, "portal", "portalpass1")
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, `{"network":"office","principal":"portal"}`); code == http.StatusForbidden {
		t.Errorf("a network-only account created over HTTP cannot administer its own network (%s)", out)
	}
}

// The allowlist is fail-closed: a network-only account is denied every route
// that is not on it, so a route added tomorrow is denied until somebody puts it
// there deliberately. /gfeh/partitions/* is the deliberate omission checked
// above; these are ordinary control-plane routes it must never reach.
func TestNetworkOnlyAccountStillDeniedTheRestOfTheControlPlane(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newNetworkOnlyAccount(t, e, []string{"office"})

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "account"},
		{http.MethodGet, "settings"},
		{http.MethodGet, "packages"},
		{http.MethodGet, "storage"},
		{http.MethodGet, "networks/peers/connected"},
	} {
		if code, out := e.do(t, r.method, r.path, token, ""); code != http.StatusForbidden {
			t.Errorf("%s /%s = %d (%s), want 403", r.method, r.path, code, out)
		}
	}
}

// --- The grants are independent ---

// The gfeh grant buys object storage and nothing else. An account holding it
// must not gain peer enrollment as a side effect: they are separate toggles,
// and a holder of one reaching the other's routes would make the split
// decorative.
func TestGfehGrantDoesNotBuyPeerEnrollment(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantGfeh}, []string{"office"})

	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, addPrincipalBody("office")); code == http.StatusForbidden {
		t.Errorf("the gfeh grant did not admit its own route: 403 (%s)", out)
	}
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "networks/peers?network=office", ""},
		{http.MethodPost, "networks/peers/add", `{"network":"office","name":"laptop"}`},
		{http.MethodPost, "networks/peers/refresh", `{"network":"office","public_key":"k"}`},
	} {
		if code, out := e.do(t, r.method, r.path, token, r.body); code != http.StatusForbidden {
			t.Errorf("gfeh-only account %s /%s = %d (%s), want 403", r.method, r.path, code, out)
		}
	}
}

// ... and the reverse. The wireguard grant buys peer enrollment and must not
// reach a partition's user database.
func TestWireGuardGrantDoesNotBuyObjectStorage(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, []string{account.GrantWireGuard}, []string{"office"})

	if code, out := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"office","name":"laptop"}`); code == http.StatusForbidden {
		t.Errorf("the wireguard grant did not admit its own route: 403 (%s)", out)
	}
	for _, m := range gfehMutators {
		if code, out := e.do(t, http.MethodPost, m.path, token, m.body); code != http.StatusForbidden {
			t.Errorf("wireguard-only account POST /%s = %d (%s), want 403", m.path, code, out)
		}
	}
}

// Holding both grants admits both sets of routes, so the toggles compose rather
// than one overriding the other.
func TestBothGrantsAdmitBothSurfaces(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, account.AllGrants, []string{"office"})

	if code, out := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"office","name":"laptop"}`); code == http.StatusForbidden {
		t.Errorf("peer enrollment denied to an account holding every grant: %s", out)
	}
	if code, out := e.do(t, http.MethodPost, "gfeh/principals/add", token, addPrincipalBody("office")); code == http.StatusForbidden {
		t.Errorf("object storage denied to an account holding every grant: %s", out)
	}
}

// The allowlist is fail-closed per grant: a grant-holder reaches the routes its
// grants unlock, the common few needed to sign in, and nothing else -- so a
// route added tomorrow is denied until somebody lists it deliberately.
func TestGrantHolderIsDeniedTheRestOfTheControlPlane(t *testing.T) {
	e := initWireGuardTestEnv(t)
	token := newGrantedAccount(t, e, account.AllGrants, []string{"office"})

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "account"},
		{http.MethodGet, "settings"},
		{http.MethodGet, "packages"},
		{http.MethodGet, "storage"},
		{http.MethodGet, "networks/peers/connected"},
	} {
		if code, out := e.do(t, r.method, r.path, token, ""); code != http.StatusForbidden {
			t.Errorf("%s /%s = %d (%s), want 403", r.method, r.path, code, out)
		}
	}
}
