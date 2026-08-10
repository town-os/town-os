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
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// GET /networks/peers is reachable by an account holding the wireguard grant,
// and it named its network in a query parameter that nothing checked against
// the caller's scope — so a portal account scoped to one network could
// enumerate every other network's devices. peers/add and peers/refresh both
// confined; only the read did not.
//
// The unit test in controller_networks_peers_scope_test.go drives the same
// path; this one runs it over a real SQLite account, session, and network store
// with peers enrolled through the HTTP API, so the scope decision is made
// against persisted rows rather than fixtures.

type peerScopeEnv struct {
	client      *systemcontroller.SystemdClient
	adminToken  string
	portalToken string
}

const peerScopePassword = "portalpass1"

// Peer keys for the scope tests. WireGuard keys are base64 of 32 bytes and the
// API validates that, so these cannot be readable labels -- and they are named
// constants rather than repeated literals precisely because the enrolled key
// and the key an assertion looks for have to be the same string. They decode to
// "LABPUBKEY"/"OFFICEPUBKEY" followed by dash padding.
const (
	labScopePeerKey    = "TEFCUFVCS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0="
	officeScopePeerKey = "T0ZGSUNFUFVCS0VZLS0tLS0tLS0tLS0tLS0tLS0tLS0="
)

func initPeerScopeTest(t *testing.T) *peerScopeEnv {
	t.Helper()

	db, err := account.OpenDB(t.Context(), filepath.Join(t.TempDir(), "peers.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
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
	nm, err := account.InitNetworkManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		AccountMgr:       mgr,
		SessionMgr:       sessMgr,
		NetworkMgr:       nm,
		Systemd:          systemd.InitMockManager(),
		NetworkStatePath: t.TempDir(),
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Two overlay networks with routable subnets so peers/add can allocate.
	for _, seed := range []struct{ name, subnet, addr string }{
		{"office", "10.90.42.0/24", "10.90.42.1/24"},
		{"lab", "10.90.43.0/24", "10.90.43.1/24"},
	} {
		if _, err := nm.Create(t.Context(), &account.Network{
			Name: seed.name, TLD: seed.name, Subnet: seed.subnet, Address: seed.addr,
			PublicKey: "PUB", PrivateKey: "PRIV", ListenPort: 51820, Enabled: true,
		}); err != nil {
			t.Fatalf("seed network %s: %v", seed.name, err)
		}
	}

	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass1", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "adminpass1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	// A portal account scoped to office only.
	if _, err := c.CreateGrantedAccount(context.TODO(), "portal", peerScopePassword, "p@test.com", "555-0001", "Portal",
		[]string{account.GrantWireGuard}, []string{"office"}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	portalResp, err := c.Authenticate(context.TODO(), "portal", peerScopePassword)
	if err != nil {
		t.Fatalf("Authenticate portal: %v", err)
	}

	return &peerScopeEnv{client: c, adminToken: adminResp.Token, portalToken: portalResp.Token}
}

func (e *peerScopeEnv) request(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.TODO(), method, e.client.BaseURL+"/"+path, reader)
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
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
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, string(out)
}

func TestSystemControllerListPeersOutOfScopeDenied(t *testing.T) {
	t.Parallel()
	e := initPeerScopeTest(t)

	// An admin enrolls a device on the network the portal account has no
	// business seeing, so a leak would have something to leak.
	if code, body := e.request(t, http.MethodPost, "networks/peers/add", e.adminToken,
		`{"network":"lab","name":"lab-laptop","public_key":"`+labScopePeerKey+`"}`); code != http.StatusOK {
		t.Fatalf("admin peers/add lab = %d (%s), want 200", code, body)
	}

	code, body := e.request(t, http.MethodGet, "networks/peers?network=lab", e.portalToken, "")
	if code != http.StatusForbidden {
		t.Fatalf("portal GET peers?network=lab = %d (%s), want 403", code, body)
	}
	if strings.Contains(body, labScopePeerKey) || strings.Contains(body, "lab-laptop") {
		t.Fatalf("out-of-scope peer leaked: %s", body)
	}
}

func TestSystemControllerListPeersInScopeAllowed(t *testing.T) {
	t.Parallel()
	e := initPeerScopeTest(t)

	if code, body := e.request(t, http.MethodPost, "networks/peers/add", e.portalToken,
		`{"network":"office","name":"phone","public_key":"`+officeScopePeerKey+`"}`); code != http.StatusOK {
		t.Fatalf("portal peers/add office = %d (%s), want 200", code, body)
	}

	code, body := e.request(t, http.MethodGet, "networks/peers?network=office", e.portalToken, "")
	if code != http.StatusOK {
		t.Fatalf("portal GET peers?network=office = %d (%s), want 200", code, body)
	}
	if !strings.Contains(body, officeScopePeerKey) {
		t.Fatalf("in-scope peer list is missing the enrolled peer: %s", body)
	}
}
