// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/wireguard"
)

// POST /networks/peers/add is reachable by a NON-ADMIN holding the wireguard
// grant (requirePeerEnroll). Its public_key and endpoint fields are stored with
// only strings.TrimSpace -- account.SQLiteNetworkManager.AddPeer checks only
// that the key is non-empty -- and are then rendered verbatim into the
// wg-quick config by wireguard.RenderInterfaceConfig:
//
//	fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
//	fmt.Fprintf(&b, "Endpoint = %s\n", p.Endpoint)
//
// That file is executed as root by the generated systemd unit:
//
//	ExecStart=/usr/bin/wg-quick up <configPath>
//
// wg-quick's parser honours PreUp/PostUp/PreDown/PostDown for every line it
// reads while it is inside an [Interface] section, and section state is driven
// by the file's own content. A field value carrying a newline followed by
// "[Interface]" therefore reopens that section and everything after it is
// interpreted as interface configuration -- including a PostUp shell command,
// which wg-quick runs as root. addNetworkPeer calls applyNetworkTransport
// immediately after the insert, which rewrites the config and restarts the
// unit, so the command fires on the same request.
//
// This crosses the boundary the grant system exists to hold: the confined
// "portal" account that may only enroll its own devices reaches root on a box
// that drives the host podman socket.
//
// These tests assert the SECURE behaviour, so they fail against the current
// code and become regressions once the input is validated.

// peerInjectionEnv is a self-contained networks fixture that keeps a handle on
// the network-state directory, so a test can read back the wg-quick config the
// controller actually wrote. initPeerScopeTest deliberately hides that path;
// duplicating the setup here leaves that fixture untouched.
type peerInjectionEnv struct {
	client      *systemcontroller.SystemdClient
	adminToken  string
	portalToken string
	stateDir    string
	networkName string
}

const peerInjectionPassword = "portalpass1"

func initPeerInjectionTest(t *testing.T) *peerInjectionEnv {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "peers.db"))
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
	nm, err := account.InitNetworkManager(db)
	if err != nil {
		t.Fatalf("InitNetworkManager: %v", err)
	}

	stateDir := t.TempDir()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		AccountMgr:       mgr,
		SessionMgr:       sessMgr,
		NetworkMgr:       nm,
		Systemd:          systemd.InitMockManager(),
		NetworkStatePath: stateDir,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// A subnet distinct from the other networks fixtures so nothing is shared
	// even in principle. No real interface is created (mock systemd), but the
	// config file is written for real.
	const network = "depot"
	if _, err := nm.Create(&account.Network{
		Name: network, TLD: network, Subnet: "10.90.51.0/24", Address: "10.90.51.1/24",
		PublicKey: "PUB", PrivateKey: "PRIV", ListenPort: 51821, Enabled: true,
	}); err != nil {
		t.Fatalf("seed network %s: %v", network, err)
	}

	if _, err := c.CreateAccount(context.TODO(), "admin", "adminpass1", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	adminResp, err := c.Authenticate(context.TODO(), "admin", "adminpass1")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = adminResp.Token

	// The confined account: a non-admin holding only the wireguard grant,
	// scoped to the one network. This is the caller the finding is about.
	if _, err := c.CreateGrantedAccount(context.TODO(), "portal", peerInjectionPassword, "p@test.com", "555-0001", "Portal",
		[]string{account.GrantWireGuard}, []string{network}); err != nil {
		t.Fatalf("CreateGrantedAccount: %v", err)
	}
	portalResp, err := c.Authenticate(context.TODO(), "portal", peerInjectionPassword)
	if err != nil {
		t.Fatalf("Authenticate portal: %v", err)
	}

	return &peerInjectionEnv{
		client:      c,
		adminToken:  adminResp.Token,
		portalToken: portalResp.Token,
		stateDir:    stateDir,
		networkName: network,
	}
}

// jsonQuote renders s as a JSON string literal, so a payload carrying newlines
// reaches the handler as one field value rather than breaking the request body.
func jsonQuote(t *testing.T, s string) string {
	t.Helper()
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal(%q): %v", s, err)
	}
	return string(out)
}

// addPeer posts one enrollment as the given bearer token and returns the
// status and body. Every case in this file drives the same route; what varies
// is who is calling and what the payload carries.
func (e *peerInjectionEnv) addPeer(t *testing.T, token, body string) (int, string) {
	t.Helper()
	const path = "networks/peers/add"
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, e.client.BaseURL+"/"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.client.HTTP.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
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

// wgConfig returns the wg-quick config the controller rendered for the fixture
// network, or "" when it has not written one. The filename is derived exactly
// as networkConfigPath derives it, through the exported interface-name helper
// so the instance salt is applied.
func (e *peerInjectionEnv) wgConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(e.stateDir, systemcontroller.NetworkInterfaceName(e.networkName)+".conf")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read wg config %s: %v", path, err)
	}
	return string(data)
}

// assertNoRootCommandInjection fails when the rendered wg-quick config carries
// a directive wg-quick would execute, or a second [Interface] section that
// would make it start honouring them.
//
// Checked regardless of what the API answered: a fix that rejects the input is
// correct, and so is one that refuses to render it, but a config on disk with a
// PostUp in it is the actual harm and must never happen either way.
func assertNoRootCommandInjection(t *testing.T, cfg, label string) {
	t.Helper()
	if cfg == "" {
		return
	}
	for _, directive := range []string{"PostUp", "PreUp", "PostDown", "PreDown", "SaveConfig", "Table"} {
		if strings.Contains(cfg, directive) {
			t.Errorf("%s: rendered wg-quick config carries an injected %s directive; wg-quick runs it as root.\n--- config ---\n%s", label, directive, cfg)
		}
	}
	if n := strings.Count(cfg, "[Interface]"); n > 1 {
		t.Errorf("%s: rendered wg-quick config has %d [Interface] sections, want 1; a second one makes wg-quick honour Post/PreUp again.\n--- config ---\n%s", label, n, cfg)
	}
}

func TestSystemControllerPeerPublicKeyCannotInjectWgQuickDirectives(t *testing.T) {
	t.Parallel()
	e := initPeerInjectionTest(t)

	// A well-formed key, then a newline that reopens the interface section and
	// hands wg-quick a command to run as root when it brings the link up.
	const payload = `kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=
[Interface]
PostUp = /bin/sh -c 'id > /tmp/town-os-pwned'`

	body := `{"network":"` + e.networkName + `","name":"laptop","public_key":` + jsonQuote(t, payload) + `}`
	code, respBody := e.addPeer(t, e.portalToken, body)

	assertNoRootCommandInjection(t, e.wgConfig(t), "public_key")

	if code == http.StatusOK {
		t.Errorf("peers/add accepted a public_key containing a newline and wg-quick directives (status %d, body %s); "+
			"a non-admin holding only the wireguard grant reaches root this way", code, respBody)
	}
}

func TestSystemControllerPeerEndpointCannotInjectWgQuickDirectives(t *testing.T) {
	t.Parallel()
	e := initPeerInjectionTest(t)

	const payload = `198.51.100.7:51820
[Interface]
PostUp = /bin/sh -c 'id > /tmp/town-os-pwned'`

	body := `{"network":"` + e.networkName + `","name":"laptop",` +
		`"public_key":"kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=","endpoint":` + jsonQuote(t, payload) + `}`
	code, respBody := e.addPeer(t, e.portalToken, body)

	assertNoRootCommandInjection(t, e.wgConfig(t), "endpoint")

	if code == http.StatusOK {
		t.Errorf("peers/add accepted an endpoint containing a newline and wg-quick directives (status %d, body %s)", code, respBody)
	}
}

// A malformed key must be refused before it reaches the config, and the
// refusal must not depend on the caller being confined -- an admin enrolling a
// device is the same write to the same root-executed file.
func TestSystemControllerPeerPublicKeyInjectionRejectedForAdminToo(t *testing.T) {
	t.Parallel()
	e := initPeerInjectionTest(t)

	const payload = "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=\n[Interface]\nPostUp = /bin/false"

	body := `{"network":"` + e.networkName + `","name":"admin-laptop","public_key":` + jsonQuote(t, payload) + `}`
	code, respBody := e.addPeer(t, e.adminToken, body)

	assertNoRootCommandInjection(t, e.wgConfig(t), "admin public_key")

	if code == http.StatusOK {
		t.Errorf("peers/add accepted an injecting public_key from an admin token (status %d, body %s)", code, respBody)
	}
}

// The counterpart to the three above: a real, well-formed WireGuard key must
// still enroll. Without this, "reject everything" would look like a fix.
//
// It also pins the shape a fix has to accept -- base64 of 32 bytes, which is
// what wireguard.GenerateKeypair emits.
func TestSystemControllerPeerAcceptsWellFormedPublicKey(t *testing.T) {
	t.Parallel()
	e := initPeerInjectionTest(t)

	_, pub, err := wireguard.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	body := `{"network":"` + e.networkName + `","name":"phone","public_key":` + jsonQuote(t, pub) + `}`
	code, respBody := e.addPeer(t, e.portalToken, body)
	if code != http.StatusOK {
		t.Fatalf("peers/add with a well-formed key = %d (%s), want 200", code, respBody)
	}

	cfg := e.wgConfig(t)
	if !strings.Contains(cfg, "PublicKey = "+pub) {
		t.Errorf("rendered config is missing the enrolled peer's key.\n--- config ---\n%s", cfg)
	}
	assertNoRootCommandInjection(t, cfg, "well-formed key")
}

// Separate from the injection above, and a smaller thing: `rolodex: true` makes
// the box forward every query for the network's TLD to an address the peer
// controls. requirePeerEnroll admits a non-admin for enrollment; nothing checks
// that the same account may also redirect name resolution for everyone else on
// that network. The grant is described as enrolling and refreshing peers.
//
// Asserted as "an admin decision", which is what the flag actually is.
func TestSystemControllerPeerRolodexFlagRequiresAdmin(t *testing.T) {
	t.Parallel()
	e := initPeerInjectionTest(t)

	_, pub, err := wireguard.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	body := `{"network":"` + e.networkName + `","name":"resolver","public_key":` + jsonQuote(t, pub) + `,"rolodex":true}`
	code, respBody := e.addPeer(t, e.portalToken, body)
	if code == http.StatusOK {
		t.Errorf("peers/add let a wireguard-grant account register itself as the network's DNS forwarder "+
			"(status %d, body %s); that redirects name resolution for every peer on the network", code, respBody)
	}
}
