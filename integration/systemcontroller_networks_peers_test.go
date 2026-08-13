// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// connectedPeersEnv is the fixture for the connected-peers endpoint tests.
type connectedPeersEnv struct {
	client   *systemcontroller.SystemdClient
	nm       *account.SQLiteNetworkManager
	db       *sql.DB
	sd       *systemd.MockManager
	stateDir string
}

// initConnectedPeersTest builds a server over a real SQLite network manager and
// a mock systemd. It exposes the network manager because the HTTP enroll path
// has no session to attribute a peer to, so tests that care about the account
// column seed their peers directly.
func initConnectedPeersTest(t *testing.T) connectedPeersEnv {
	t.Helper()

	nm, db := initNetworkDBHandle(t)
	sd := systemd.InitMockManager()
	stateDir := t.TempDir()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		NetworkStatePath: stateDir,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}
	return connectedPeersEnv{client: c, nm: nm, db: db, sd: sd, stateDir: stateDir}
}

// insertLegacyPeer writes a peer row straight into the table, around every rule
// AddPeer enforces. It exists for exactly one row: a peer on the DNS-only home
// network, which AddPeer refuses now but which an install predating that
// refusal still has on disk. Readers must keep filtering those rows out, and
// there is no supported call that can produce one to test against.
func insertLegacyPeer(t *testing.T, db *sql.DB, network, publicKey, name, allowedIP string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO network_peers
		(network_name, public_key, name, allowed_ip, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		network, publicKey, name, allowedIP, "admin", time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert legacy peer on %s: %v", network, err)
	}
}

// findConnectedPeer returns the view for a peer by network and name.
func findConnectedPeer(peers []systemcontroller.ConnectedPeerView, network, name string) (systemcontroller.ConnectedPeerView, bool) {
	for _, p := range peers {
		if p.Network == network && p.Name == name {
			return p, true
		}
	}
	return systemcontroller.ConnectedPeerView{}, false
}

// TestConnectedPeersHTTPListsEnrolledPeers drives GET /networks/peers/connected
// end-to-end through the router: peers enrolled across two WireGuard networks
// are itemized with the account that enrolled them, their overlay address, and
// their expiry.
//
// There is no live WireGuard device in the test container, so the live half is
// necessarily absent — which is itself the assertion that matters here: the
// panel must degrade to the persisted rows rather than erroring or emptying
// when `wg show` cannot run. The live merge itself is unit-tested against a
// stubbed dump in controller_networks_peers_test.go.
func TestConnectedPeersHTTPListsEnrolledPeers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := initConnectedPeersTest(t)
	c, nm := env.client, env.nm

	for _, name := range []string{"office", "lab"} {
		if _, err := c.CreateNetwork(ctx, name, name); err != nil {
			t.Fatalf("CreateNetwork %s: %v", name, err)
		}
	}

	// Seed peers directly so created_by and expires_at are set: the HTTP enroll
	// path has no calling account on this server, so it would leave both empty.
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	seed := []*account.NetworkPeer{
		{Network: "office", PublicKey: "k-laptop", Name: "laptop", AllowedIP: "10.90.12.2/32", CreatedBy: "alice"},
		{Network: "office", PublicKey: "k-portal", Name: "portal", AllowedIP: "10.90.12.3/32", CreatedBy: "wg-portal", ExpiresAt: &exp},
		{Network: "lab", PublicKey: "k-bench", Name: "bench", AllowedIP: "10.90.13.2/32", CreatedBy: "bob"},
	}
	for _, p := range seed {
		if _, err := nm.AddPeer(t.Context(), p); err != nil {
			t.Fatalf("seed peer %s: %v", p.Name, err)
		}
	}

	peers, err := c.ListConnectedPeers(ctx)
	if err != nil {
		t.Fatalf("ListConnectedPeers: %v", err)
	}
	if len(peers) != 3 {
		t.Fatalf("got %d peers, want 3: %+v", len(peers), peers)
	}

	laptop, ok := findConnectedPeer(peers, "office", "laptop")
	if !ok {
		t.Fatalf("office/laptop missing from %+v", peers)
	}
	if laptop.Account != "alice" {
		t.Errorf("laptop account = %q, want alice", laptop.Account)
	}
	if laptop.AllowedIP != "10.90.12.2/32" {
		t.Errorf("laptop overlay ip = %q, want 10.90.12.2/32", laptop.AllowedIP)
	}
	if laptop.TLD != "office" {
		t.Errorf("laptop tld = %q, want office", laptop.TLD)
	}
	if laptop.Interface != systemcontroller.NetworkInterfaceName("office") {
		t.Errorf("laptop interface = %q, want %q", laptop.Interface, systemcontroller.NetworkInterfaceName("office"))
	}
	if laptop.ExpiresAt != nil {
		t.Errorf("laptop expiry = %v, want nil (admin enrollments are permanent)", laptop.ExpiresAt)
	}

	portal, ok := findConnectedPeer(peers, "office", "portal")
	if !ok {
		t.Fatalf("office/portal missing from %+v", peers)
	}
	if portal.ExpiresAt == nil {
		t.Error("portal expiry is nil, want the seeded TTL")
	}

	bench, ok := findConnectedPeer(peers, "lab", "bench")
	if !ok {
		t.Fatalf("lab/bench missing from %+v", peers)
	}
	if bench.Account != "bob" {
		t.Errorf("bench account = %q, want bob", bench.Account)
	}

	// No live device exists here, so nothing may claim to be connected.
	for _, p := range peers {
		if p.Connected {
			t.Errorf("peer %q reports connected with no wireguard device present", p.Name)
		}
		if p.LastHandshake != nil {
			t.Errorf("peer %q has a handshake with no device present: %v", p.Name, p.LastHandshake)
		}
	}
}

// The default network is a DNS-only scope with no WireGuard transport, so it
// can never have a tunnelled peer. Even a row sitting on it (a legacy install)
// must not surface in a panel about who is tunnelled in.
func TestConnectedPeersHTTPExcludesHomeNetwork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := initConnectedPeersTest(t)
	c, nm := env.client, env.nm

	// The home network is not created here: it always exists (InitNetworkManager
	// seeds it), and POST /networks/create refuses the duplicate.
	if _, err := c.CreateNetwork(ctx, "office", "office"); err != nil {
		t.Fatalf("CreateNetwork office: %v", err)
	}
	// Written around AddPeer, which refuses the home network now: this row is
	// the legacy install, and the filter under test is what keeps it out of the
	// panel.
	insertLegacyPeer(t, env.db, account.DefaultNetworkName, "k-legacy", "legacy", "10.90.1.2/32")
	if _, err := nm.AddPeer(t.Context(), &account.NetworkPeer{
		Network: "office", PublicKey: "k-laptop",
		Name: "laptop", AllowedIP: "10.90.12.2/32", CreatedBy: "alice",
	}); err != nil {
		t.Fatalf("seed office peer: %v", err)
	}

	peers, err := c.ListConnectedPeers(ctx)
	if err != nil {
		t.Fatalf("ListConnectedPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("got %d peers, want only the office one: %+v", len(peers), peers)
	}
	if peers[0].Network != "office" {
		t.Errorf("peer network = %q, want office", peers[0].Network)
	}
}

// TestConnectedPeersHTTPDisconnectRevokesPeer is the disconnect button's
// end-to-end contract: the peer leaves the panel, leaves the rendered interface
// config, and the interface is bounced so the kernel actually drops it. A peer
// that vanishes from the API while its key is still in the live config would be
// a disconnect button that does not disconnect.
func TestConnectedPeersHTTPDisconnectRevokesPeer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := initConnectedPeersTest(t)
	c, sd, stateDir := env.client, env.sd, env.stateDir

	if _, err := c.CreateNetwork(ctx, "office", "office"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	laptop, err := c.AddNetworkPeer(ctx, systemcontroller.AddNetworkPeerRequest{Network: "office", Name: "laptop"})
	if err != nil {
		t.Fatalf("AddNetworkPeer laptop: %v", err)
	}
	phone, err := c.AddNetworkPeer(ctx, systemcontroller.AddNetworkPeerRequest{Network: "office", Name: "phone"})
	if err != nil {
		t.Fatalf("AddNetworkPeer phone: %v", err)
	}

	peers, err := c.ListConnectedPeers(ctx)
	if err != nil {
		t.Fatalf("ListConnectedPeers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers before disconnect, want 2", len(peers))
	}

	iface := systemcontroller.NetworkInterfaceName("office")
	cfgPath := filepath.Join(stateDir, iface+".conf")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read interface config: %v", err)
	}
	if !strings.Contains(string(cfg), laptop.Peer.PublicKey) {
		t.Fatalf("interface config missing laptop key before disconnect:\n%s", cfg)
	}

	// Disconnect the laptop.
	sd.ClearCalls()
	if rerr := c.RemoveNetworkPeer(ctx, "office", laptop.Peer.PublicKey); rerr != nil {
		t.Fatalf("RemoveNetworkPeer: %v", rerr)
	}

	// It is gone from the panel, and the phone is untouched.
	peers, err = c.ListConnectedPeers(ctx)
	if err != nil {
		t.Fatalf("ListConnectedPeers after disconnect: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("got %d peers after disconnect, want 1: %+v", len(peers), peers)
	}
	if peers[0].PublicKey != phone.Peer.PublicKey {
		t.Errorf("surviving peer = %q, want the phone %q", peers[0].PublicKey, phone.Peer.PublicKey)
	}

	// Its key is gone from the rendered interface config...
	cfg, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read interface config after disconnect: %v", err)
	}
	if strings.Contains(string(cfg), laptop.Peer.PublicKey) {
		t.Errorf("disconnected peer's key still in the interface config:\n%s", cfg)
	}
	if !strings.Contains(string(cfg), phone.Peer.PublicKey) {
		t.Errorf("surviving peer's key dropped from the interface config:\n%s", cfg)
	}

	// ...and the interface was bounced, which is what makes the kernel forget it.
	// Without this the config on disk and the live device disagree until the next
	// reconcile, and the revoked device keeps routing.
	unitName := systemd.NetworkUnitName("office")
	if act := lastStatusAction(sd, unitName); act != string(systemd.Restart) {
		t.Errorf("expected the interface to be restarted after a disconnect, got %q", act)
	}
}

// Disconnecting an unknown peer is a 404, not a silent success. A UI that
// reported success here would tell the operator a device was cut off when it
// was not.
func TestConnectedPeersHTTPDisconnectUnknownPeerIs404(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := initConnectedPeersTest(t).client

	if _, err := c.CreateNetwork(ctx, "office", "office"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if err := c.RemoveNetworkPeer(ctx, "office", "no-such-key"); err == nil {
		t.Error("expected an error disconnecting a peer that does not exist")
	}
}

// A box with no WireGuard networks renders an empty panel rather than erroring.
func TestConnectedPeersHTTPEmptyWithNoNetworks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := initConnectedPeersTest(t).client

	peers, err := c.ListConnectedPeers(ctx)
	if err != nil {
		t.Fatalf("ListConnectedPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("got %d peers, want 0: %+v", len(peers), peers)
	}
}
