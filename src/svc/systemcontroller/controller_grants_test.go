// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/storage"
)

// wgTestEnv is a full HTTP server wired with account, session, audit, settings,
// and a real SQLite network manager, plus a bootstrapped admin token and two
// seeded overlay networks ("office" and "lab").
type wgTestEnv struct {
	client     *SystemdClient
	nm         account.NetworkManager
	settings   account.SettingsManager
	adminToken string
}

func initWireGuardTestEnv(t *testing.T) *wgTestEnv {
	t.Helper()
	return initWireGuardTestEnvWith(t, nil)
}

// initWireGuardTestEnvWith is initWireGuardTestEnv with a hook that may adjust
// the ServerConfig before the server starts — the only way to give the env
// something the plain constructor leaves nil (a gfeh registry, say) while still
// driving the real router, so the middleware chain under test is the shipped
// one rather than a handler called directly.
func initWireGuardTestEnvWith(t *testing.T, tweak func(*ServerConfig)) *wgTestEnv {
	t.Helper()
	db, err := account.OpenDB(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := account.InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	auditMgr, err := account.InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}
	settingsMgr, err := account.InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}
	nm, err := account.InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}

	cfg := ServerConfig{
		Storage:     storage.InitBtrFSMock(),
		AccountMgr:  mgr,
		SessionMgr:  sessMgr,
		AuditMgr:    auditMgr,
		SettingsMgr: settingsMgr,
		NetworkMgr:  nm,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	ts := InitTestServer(cfg)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Bootstrap admin (no auth required on empty DB) and authenticate.
	if _, err := c.CreateAccount(context.Background(), "testadmin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.Background(), "testadmin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	// Seed two overlay networks with routable subnets so peers/add can allocate.
	for _, seed := range []struct{ name, subnet, addr string }{
		{"office", "10.90.12.0/24", "10.90.12.1/24"},
		{"lab", "10.90.13.0/24", "10.90.13.1/24"},
	} {
		if _, err := nm.Create(t.Context(), &account.Network{
			Name: seed.name, TLD: seed.name, Subnet: seed.subnet, Address: seed.addr,
			PublicKey: "PUB", PrivateKey: "PRIV", ListenPort: 51820, Enabled: true,
		}); err != nil {
			t.Fatalf("seed network %s: %v", seed.name, err)
		}
	}

	return &wgTestEnv{client: c, nm: nm, settings: settingsMgr, adminToken: resp.Token}
}

// do issues a JSON request with an optional bearer token and returns the status
// code and body. The caller owns neither — the body is fully read and closed.
func (e *wgTestEnv) do(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.client.route(path), strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.HTTP.Do(req)
	if err != nil {
		t.Fatalf("HTTP %s %s: %v", method, path, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(out)
}

// authToken creates (as admin) and authenticates an account, returning its token.
func (e *wgTestEnv) authToken(t *testing.T, username, password string) string {
	t.Helper()
	resp, err := e.client.Authenticate(context.Background(), username, password)
	if err != nil {
		t.Fatalf("Authenticate %s: %v", username, err)
	}
	return resp.Token
}

// --- Account creation over HTTP ---

func TestHTTPCreateGrantedAccount(t *testing.T) {
	e := initWireGuardTestEnv(t)

	acct, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"})
	if err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	if !acct.HasGrant(account.GrantWireGuard) || !acct.HasGrant(account.GrantGfeh) || acct.Admin {
		t.Errorf("account grants=%v Admin=%v, want both grants and a non-admin", acct.Grants, acct.Admin)
	}
	if len(acct.Networks) != 1 || acct.Networks[0] != "office" {
		t.Errorf("Networks = %v, want [office]", acct.Networks)
	}
}

func TestHTTPCreateGrantedAccountEmptyScopeRejected(t *testing.T) {
	e := initWireGuardTestEnv(t)

	_, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, nil)
	if err == nil {
		t.Fatal("expected error creating a network-only account with empty scope")
	}
}

// --- Fail-closed allowlist ---

func TestGrantAllowlistConfinesAccount(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	token := e.authToken(t, "portal", "portalpass1")

	// Allowlisted read endpoints succeed.
	if code, body := e.do(t, http.MethodGet, "networks", token, ""); code != http.StatusOK {
		t.Errorf("GET /networks = %d (%s), want 200", code, body)
	}
	if code, _ := e.do(t, http.MethodGet, "account/me", token, ""); code != http.StatusOK {
		t.Errorf("GET /account/me = %d, want 200", code)
	}

	// The object-storage routes are on the allowlist now, because administering
	// the storage a network owns is the other half of what "network-only"
	// means. 503 is the authorized answer here (no gfeh registry), so anything
	// other than 403 means the gate let it through.
	if code, body := e.do(t, http.MethodGet, "gfeh", token, ""); code == http.StatusForbidden {
		t.Errorf("GET /gfeh = 403 (%s), want it admitted", body)
	}
	if code, body := e.do(t, http.MethodPost, "gfeh/principals/add", token, `{"network":"office","principal":"portal"}`); code == http.StatusForbidden {
		t.Errorf("POST /gfeh/principals/add on its own network = 403 (%s), want it admitted", body)
	}
	// ... but provisioning a partition is not, and stays admin-only.
	if code, _ := e.do(t, http.MethodPost, "gfeh/partitions/create", token, `{"name":"office","quota":0}`); code != http.StatusForbidden {
		t.Errorf("POST /gfeh/partitions/create = %d, want 403", code)
	}

	// A non-allowlisted endpoint is denied even though it is a plain requireAuth
	// route a normal account could reach.
	if code, _ := e.do(t, http.MethodGet, "account", token, ""); code != http.StatusForbidden {
		t.Errorf("GET /account (list) = %d, want 403", code)
	}
	// A privileged endpoint outside the allowlist is denied (fail-closed), not
	// merely 403'd by requireAdmin — the global gate catches it first.
	if code, _ := e.do(t, http.MethodPost, "networks/create", token, `{"name":"x"}`); code != http.StatusForbidden {
		t.Errorf("POST /networks/create = %d, want 403", code)
	}
	if code, _ := e.do(t, http.MethodPost, "networks/peers/remove", token, `{"network":"office","public_key":"k"}`); code != http.StatusForbidden {
		t.Errorf("POST /networks/peers/remove = %d, want 403", code)
	}
}

func TestGrantAllowlistLeavesNormalAccountsAlone(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateAccount(context.Background(), "alice", "alicepass1", "a@test.com", "555-2", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	token := e.authToken(t, "alice", "alicepass1")

	// A normal account still reaches its requireAuth endpoints; the allowlist
	// only constrains network-only accounts.
	if code, _ := e.do(t, http.MethodGet, "account", token, ""); code != http.StatusOK {
		t.Errorf("GET /account for normal user = %d, want 200", code)
	}
}

// --- Peer enrollment: scope + TTL + attribution ---

func TestWireGuardPeerEnrollWithinScope(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	token := e.authToken(t, "portal", "portalpass1")

	// In-scope enrollment succeeds.
	if code, body := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"office","public_key":"UE9SVEFMS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusOK {
		t.Fatalf("peers/add office = %d (%s), want 200", code, body)
	}

	// The stored peer is attributed to the account and carries a TTL.
	peers, err := e.nm.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("want 1 peer, got %d", len(peers))
	}
	if peers[0].CreatedBy != "portal" {
		t.Errorf("CreatedBy = %q, want portal", peers[0].CreatedBy)
	}
	if peers[0].ExpiresAt == nil {
		t.Error("enrolled peer has no TTL")
	} else if !peers[0].ExpiresAt.After(time.Now()) {
		t.Errorf("TTL %v is not in the future", peers[0].ExpiresAt)
	}
}

func TestWireGuardPeerEnrollOutOfScopeDenied(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	token := e.authToken(t, "portal", "portalpass1")

	// "lab" is a real network but outside this account's scope.
	if code, _ := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"lab","public_key":"T1VUT0ZTQ09QRS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusForbidden {
		t.Errorf("peers/add lab = %d, want 403", code)
	}
	if peers, err := e.nm.ListPeers(t.Context(), "lab"); err != nil || len(peers) != 0 {
		t.Errorf("out-of-scope enrollment leaked a peer: %+v (err %v)", peers, err)
	}
}

func TestAdminPeerEnrollHasNoTTL(t *testing.T) {
	e := initWireGuardTestEnv(t)
	// Admin (the bootstrap token) enrolls a peer: it stays permanent.
	if code, body := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken, `{"network":"office","public_key":"QURNSU5LRVktLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusOK {
		t.Fatalf("admin peers/add = %d (%s), want 200", code, body)
	}
	peers, err := e.nm.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].ExpiresAt != nil {
		t.Errorf("admin-enrolled peer should be permanent, got %+v", peers)
	}
}

func TestPeerEnrollRejectsPlainAccount(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateAccount(context.Background(), "alice", "alicepass1", "a@test.com", "555-2", "Alice", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	token := e.authToken(t, "alice", "alicepass1")

	// A normal (non-admin, non-WireGuard) account may not manage peers.
	if code, _ := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"office","public_key":"T1VUT0ZTQ09QRS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusForbidden {
		t.Errorf("plain-account peers/add = %d, want 403", code)
	}
}

// --- Peer refresh: ownership ---

func TestWireGuardPeerRefreshOwnership(t *testing.T) {
	e := initWireGuardTestEnv(t)
	if _, err := e.client.CreateGrantedAccount(context.Background(), "portal", "portalpass1", "p@test.com", "555-1", "Portal", account.AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	token := e.authToken(t, "portal", "portalpass1")

	// Enroll the portal's own peer.
	if code, _ := e.do(t, http.MethodPost, "networks/peers/add", token, `{"network":"office","public_key":"UE9SVEFMS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusOK {
		t.Fatalf("peers/add failed")
	}
	before, err := e.nm.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	firstExpiry := *before[0].ExpiresAt

	// Refreshing its own peer slides the TTL forward.
	if code, body := e.do(t, http.MethodPost, "networks/peers/refresh", token, `{"network":"office","public_key":"UE9SVEFMS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusOK {
		t.Fatalf("refresh own peer = %d (%s), want 200", code, body)
	}
	after, err := e.nm.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if !after[0].ExpiresAt.After(firstExpiry) && !after[0].ExpiresAt.Equal(firstExpiry) {
		t.Errorf("expiry moved backwards: %v -> %v", firstExpiry, after[0].ExpiresAt)
	}

	// An admin enrolls a peer the portal does not own.
	if code, _ := e.do(t, http.MethodPost, "networks/peers/add", e.adminToken, `{"network":"office","public_key":"QURNSU5LRVktLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusOK {
		t.Fatalf("admin peers/add failed")
	}
	// The portal may not refresh a peer it did not enroll.
	if code, _ := e.do(t, http.MethodPost, "networks/peers/refresh", token, `{"network":"office","public_key":"QURNSU5LRVktLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusForbidden {
		t.Errorf("refresh unowned peer = %d, want 403", code)
	}
	// A peer that does not exist is indistinguishable from unowned (no leak).
	if code, _ := e.do(t, http.MethodPost, "networks/peers/refresh", token, `{"network":"office","public_key":"R0hPU1QtLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusForbidden {
		t.Errorf("refresh ghost peer = %d, want 403", code)
	}
	// Out-of-scope refresh is denied.
	if code, _ := e.do(t, http.MethodPost, "networks/peers/refresh", token, `{"network":"lab","public_key":"UE9SVEFMS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusForbidden {
		t.Errorf("refresh out-of-scope = %d, want 403", code)
	}

	// Admin may refresh any peer, owned attribution notwithstanding.
	if code, body := e.do(t, http.MethodPost, "networks/peers/refresh", e.adminToken, `{"network":"office","public_key":"UE9SVEFMS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusOK {
		t.Errorf("admin refresh any peer = %d (%s), want 200", code, body)
	}
}

func TestRefreshMissingPeerAsAdmin(t *testing.T) {
	e := initWireGuardTestEnv(t)
	// Admin refresh of a truly-absent peer surfaces 404 (admin has no ownership
	// gate to mask it behind a 403).
	if code, _ := e.do(t, http.MethodPost, "networks/peers/refresh", e.adminToken, `{"network":"office","public_key":"Tk9QRS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="}`); code != http.StatusNotFound {
		t.Errorf("admin refresh absent peer = %d, want 404", code)
	}
}

// --- Reaper handler ---

func TestReapExpiredPeersHandler(t *testing.T) {
	e := initWireGuardTestEnv(t)

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	if _, err := e.nm.AddPeer(t.Context(), &account.NetworkPeer{Network: "office", PublicKey: "DEAD", AllowedIP: "10.90.12.2/32", CreatedBy: "portal", ExpiresAt: &past}); err != nil {
		t.Fatalf("AddPeer dead: %v", err)
	}
	if _, err := e.nm.AddPeer(t.Context(), &account.NetworkPeer{Network: "office", PublicKey: "LIVE", AllowedIP: "10.90.12.3/32", CreatedBy: "portal", ExpiresAt: &future}); err != nil {
		t.Fatalf("AddPeer live: %v", err)
	}

	h := &SystemControllerHandlers{
		Controller: &serverBase{ServerConfig: ServerConfig{NetworkMgr: e.nm, SettingsMgr: e.settings}},
		ctx:        context.Background(),
	}
	h.reapExpiredPeers(context.Background())

	peers, err := e.nm.ListPeers(t.Context(), "office")
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].PublicKey != "LIVE" {
		t.Errorf("after reap: %+v, want just LIVE", peers)
	}
}

// --- peerTTL setting resolution ---

func TestPeerTTLReadsSetting(t *testing.T) {
	e := initWireGuardTestEnv(t)
	h := &SystemControllerHandlers{
		Controller: &serverBase{ServerConfig: ServerConfig{SettingsMgr: e.settings}},
		ctx:        context.Background(),
	}
	// Default seed is 7200s == 2h.
	if got := h.peerTTL(t.Context()); got != 2*time.Hour {
		t.Errorf("default peerTTL = %v, want 2h", got)
	}
	// A changed setting is honored.
	if err := e.settings.Set(t.Context(), "peer_ttl", "60"); err != nil {
		t.Fatalf("Set peer_ttl: %v", err)
	}
	if got := h.peerTTL(t.Context()); got != time.Minute {
		t.Errorf("peerTTL after set = %v, want 1m", got)
	}
	// A corrupt setting falls back to two hours, never zero.
	if err := e.settings.Set(t.Context(), "peer_ttl", "garbage"); err != nil {
		t.Fatalf("Set peer_ttl: %v", err)
	}
	if got := h.peerTTL(t.Context()); got != 2*time.Hour {
		t.Errorf("peerTTL on corrupt setting = %v, want 2h fallback", got)
	}
}

func TestPeerReapInterval(t *testing.T) {
	cases := []struct {
		ttl  time.Duration
		want time.Duration
	}{
		{2 * time.Hour, 15 * time.Minute}, // TTL/4 == 30m, clamped to 15m
		{20 * time.Minute, 5 * time.Minute},
		{2 * time.Minute, time.Minute}, // TTL/4 == 30s, clamped up to 1m
		{time.Second, time.Minute},
	}
	for _, tc := range cases {
		if got := peerReapInterval(tc.ttl); got != tc.want {
			t.Errorf("peerReapInterval(%v) = %v, want %v", tc.ttl, got, tc.want)
		}
	}
}
