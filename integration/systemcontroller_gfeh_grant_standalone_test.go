// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// An account holding `gfeh` and nothing else, end to end: a real SQLite account
// and session store, a real JWT round trip, the real middleware chain, and a
// real partition registry.
//
// The unit tests prove each layer decides correctly in isolation. What only an
// end-to-end pass can show is whether the layers compose into an account that
// can actually be used — the grant allowlist is a global fail-closed gate that
// runs before every route, so a route nobody thought to declare fails here and
// nowhere else. That is exactly how /status/ping came to refuse a valid token
// while serving anonymous callers: no single layer was wrong about it.
//
// `wireguard` is deliberately not granted. The two are independent, and neither
// is a prerequisite for signing in.

type gfehGrantEnv struct {
	client     *systemcontroller.SystemdClient
	adminToken string
	grantToken string
}

const (
	gfehGrantAdminPassword = "adminpass1"
	gfehGrantUserPassword  = "shelfpass1"
	gfehGrantUsername      = "shelf"
)

func initGfehGrantTest(t *testing.T) *gfehGrantEnv {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "gfehgrant.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})

	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	nm, err := account.InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}
	// A second network, so the scope the grant is confined to is a real choice
	// rather than the only one on the box.
	if _, err := nm.Create(t.Context(), &account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("create office network: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:    storage.InitBtrFSMock(),
		AccountMgr: mgr,
		SessionMgr: sessMgr,
		NetworkMgr: nm,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap window: the first administrator is created without a token.
	if _, err := c.CreateAccount(context.TODO(), "admin", gfehGrantAdminPassword, "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", gfehGrantAdminPassword)
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	acct, err := c.CreateGrantedAccount(context.TODO(), gfehGrantUsername, gfehGrantUserPassword,
		gfehGrantUsername+"@test.com", "555-0002", "Shelf",
		[]string{account.GrantGfeh}, []string{"office"})
	if err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	if acct.HasGrant(account.GrantWireGuard) {
		t.Fatalf("precondition: the account was given %q too; this test is about the gfeh grant alone",
			account.GrantWireGuard)
	}

	grantResp, err := c.Authenticate(context.TODO(), gfehGrantUsername, gfehGrantUserPassword)
	if err != nil {
		t.Fatalf("Authenticate %s: %v", gfehGrantUsername, err)
	}

	return &gfehGrantEnv{client: c, adminToken: adminResp.Token, grantToken: grantResp.Token}
}

func (e *gfehGrantEnv) do(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.TODO(), method, e.client.BaseURL+"/"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.client.HTTP.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
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

// TestIntegrationGfehGrantAloneCanSignInAndUseTheBox walks the sequence a
// browser actually performs, in order. Each step is what the next one depends
// on, which is why they are one test rather than five: the failure being
// guarded against is not "route X returns 403", it is "the account cannot get
// far enough to reach route X".
func TestIntegrationGfehGrantAloneCanSignInAndUseTheBox(t *testing.T) {
	t.Parallel()
	e := initGfehGrantTest(t)

	// 1. The session heartbeat. The dashboard polls this every 60s and tears
	//    the session down when the reply carries no username; it is a public
	//    route, so refusing a valid token here made authenticating strictly
	//    worse than presenting nothing at all.
	code, body := e.do(t, http.MethodGet, "status/ping", e.grantToken, "")
	if code != http.StatusOK {
		t.Fatalf("GET /status/ping with a gfeh-only token = %d (%s), want 200", code, body)
	}
	if !strings.Contains(body, `"username":"`+gfehGrantUsername+`"`) {
		t.Errorf("ping did not identify the caller, so the UI would log the session out: %s", body)
	}

	// 2. Who am I, and which networks am I scoped to — the object storage
	//    screen renders its network selector from this.
	for _, path := range []string{"account/me", "networks"} {
		if code, body := e.do(t, http.MethodGet, path, e.grantToken, ""); code != http.StatusOK {
			t.Errorf("GET /%s = %d (%s), want 200", path, code, body)
		}
	}

	// 3. The partition list the screen is built on.
	if code, body := e.do(t, http.MethodGet, "gfeh", e.grantToken, ""); code == http.StatusForbidden {
		t.Errorf("GET /gfeh = 403 (%s): the grant did not admit its own route", body)
	}

	// 4. And the administrative surface inside a partition on its own network.
	//    Whatever the handler makes of it (503 with no daemon answering, 404
	//    for an unknown partition) is not this test's business; 403 is, since
	//    that is the grant being refused rather than the request failing.
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "gfeh/principals?network=office", ""},
		{http.MethodPost, "gfeh/principals/add", `{"network":"office","principal":"someone"}`},
		{http.MethodGet, "gfeh/grants?network=office&principal=someone", ""},
		{http.MethodGet, "gfeh/exposures?network=office", ""},
	} {
		if code, body := e.do(t, r.method, r.path, e.grantToken, r.body); code == http.StatusForbidden {
			t.Errorf("%s /%s = 403 (%s): the gfeh grant should carry this without a wireguard grant",
				r.method, r.path, body)
		}
	}
}

// The grant is scoped and additive, not a general key. Pinned end to end
// because these are three different mechanisms — the network scope inside the
// handler, the other grant's middleware, and the global allowlist — and a
// change to any one of them shows up here.
func TestIntegrationGfehGrantAloneStaysWithinItsBounds(t *testing.T) {
	t.Parallel()
	e := initGfehGrantTest(t)

	for _, r := range []struct {
		name         string
		method, path string
		body         string
	}{
		// Scoped to office; home belongs to somebody else.
		{"another network", http.MethodGet, "gfeh/principals?network=" + account.DefaultNetworkName, ""},
		// Provisioning a partition is admin-only: it creates the root of a
		// permission tree and allocates a subvolume.
		{"partition provisioning", http.MethodPost, "gfeh/partitions/create", `{"name":"office"}`},
		// The other grant, which this account does not hold.
		{"peer enrollment", http.MethodPost, "networks/peers/add", `{"network":"office","name":"laptop"}`},
		// Every account's peers across every network.
		{"connected peers", http.MethodGet, "networks/peers/connected", ""},
		// Nothing to do with object storage.
		{"settings", http.MethodGet, "settings", ""},
	} {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if code, body := e.do(t, r.method, r.path, e.grantToken, r.body); code != http.StatusForbidden {
				t.Errorf("%s /%s = %d (%s), want 403", r.method, r.path, code, body)
			}
		})
	}
}

// The partition volume prefix is the one thing the grant must never be able to
// reach through the ordinary storage API, which rewrites every submitted name
// under user/. Included here because an account that can administer a
// partition's users is exactly the one that would try.
func TestIntegrationGfehGrantAloneCannotReachStorage(t *testing.T) {
	t.Parallel()
	e := initGfehGrantTest(t)

	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "storage/create", `{"name":"` + gfeh.VolumePrefix + `/office"}`},
		{http.MethodPost, "storage", `{"name":""}`},
	} {
		if code, body := e.do(t, r.method, r.path, e.grantToken, r.body); code != http.StatusForbidden {
			t.Errorf("%s /%s = %d (%s), want 403", r.method, r.path, code, body)
		}
	}
}
