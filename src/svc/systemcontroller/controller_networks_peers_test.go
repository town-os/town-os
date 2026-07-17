package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/wireguard"
	"github.com/labstack/echo/v5"
)

// stubWGDump swaps the `wg show` exec for a canned per-interface dump and
// restores the real one when the test ends. Unit tests have no WireGuard device
// to read, so this is the seam that makes the live-merge testable at all.
func stubWGDump(t *testing.T, dumps map[string]string) {
	t.Helper()
	prev := wgShowDump
	wgShowDump = func(_ context.Context, iface string) (string, error) {
		dump, ok := dumps[iface]
		if !ok {
			return "", fmt.Errorf("no such interface: %s", iface)
		}
		return dump, nil
	}
	t.Cleanup(func() { wgShowDump = prev })
}

// dumpLine renders one peer line in `wg show dump` format.
func dumpLine(pubKey, endpoint, allowedIPs string, handshake int64, rx, tx uint64) string {
	ep := endpoint
	if ep == "" {
		ep = "(none)"
	}
	return fmt.Sprintf("%s\t(none)\t%s\t%s\t%d\t%d\t%d\toff\n", pubKey, ep, allowedIPs, handshake, rx, tx)
}

const ifaceHeader = "privkey\tpubkey\t51820\toff\n"

// callConnectedPeers drives GET /networks/peers/connected through the handler
// and decodes the result.
func callConnectedPeers(t *testing.T, s *SystemControllerHandlers) []ConnectedPeerView {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/networks/peers/connected", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := s.listConnectedPeers(c); err != nil {
		t.Fatalf("listConnectedPeers: %v", err)
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out []ConnectedPeerView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, rec.Body.String())
	}
	return out
}

// seedOfficeNetwork creates an enabled non-default network with two peers
// enrolled by different accounts.
func seedOfficeNetwork(t *testing.T) *account.MockNetworkManager {
	t.Helper()
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{
		Name: "office", TLD: "office", Subnet: "10.90.12.0/24",
		Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "office", PublicKey: "k-laptop", Name: "laptop",
		AllowedIP: "10.90.12.2/32", CreatedBy: "alice",
	}); err != nil {
		t.Fatalf("add laptop peer: %v", err)
	}
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "office", PublicKey: "k-phone", Name: "phone",
		AllowedIP: "10.90.12.3/32", CreatedBy: "bob",
	}); err != nil {
		t.Fatalf("add phone peer: %v", err)
	}
	return mock
}

func TestListConnectedPeersJoinsLiveStatus(t *testing.T) {
	mock := seedOfficeNetwork(t)
	iface := wireguard.InterfaceName("office")
	// laptop handshook just now; phone has never handshook.
	stubWGDump(t, map[string]string{
		iface: ifaceHeader +
			dumpLine("k-laptop", "203.0.113.9:48123", "10.90.12.2/32", time.Now().Unix(), 4096, 2048) +
			dumpLine("k-phone", "", "10.90.12.3/32", 0, 0, 0),
	})

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2: %+v", len(got), got)
	}

	// Sorted by name: laptop then phone.
	laptop, phone := got[0], got[1]
	if laptop.Name != "laptop" || phone.Name != "phone" {
		t.Fatalf("unexpected order: %q, %q", laptop.Name, phone.Name)
	}

	if !laptop.Connected {
		t.Error("laptop handshook just now but reports disconnected")
	}
	if laptop.Endpoint != "203.0.113.9:48123" {
		t.Errorf("laptop endpoint = %q, want the observed 203.0.113.9:48123", laptop.Endpoint)
	}
	if laptop.RxBytes != 4096 || laptop.TxBytes != 2048 {
		t.Errorf("laptop transfer = rx %d/tx %d, want 4096/2048", laptop.RxBytes, laptop.TxBytes)
	}
	if laptop.LastHandshake == nil {
		t.Error("laptop last handshake is nil, want a timestamp")
	}
	if laptop.Account != "alice" {
		t.Errorf("laptop account = %q, want alice", laptop.Account)
	}
	if laptop.AllowedIP != "10.90.12.2/32" {
		t.Errorf("laptop overlay ip = %q, want 10.90.12.2/32", laptop.AllowedIP)
	}
	if laptop.Interface != iface {
		t.Errorf("laptop interface = %q, want %q", laptop.Interface, iface)
	}
	if laptop.TLD != "office" {
		t.Errorf("laptop tld = %q, want office", laptop.TLD)
	}

	if phone.Connected {
		t.Error("phone never handshook but reports connected")
	}
	if phone.LastHandshake != nil {
		t.Errorf("phone last handshake = %v, want nil for never-handshook", phone.LastHandshake)
	}
	if phone.Account != "bob" {
		t.Errorf("phone account = %q, want bob", phone.Account)
	}
}

// A handshake older than the stale window means the peer is enrolled but not
// here. It must still appear — the panel lists who is allowed on, with live
// state as an attribute, not a filter.
func TestListConnectedPeersStaleHandshakeIsDisconnectedButListed(t *testing.T) {
	mock := seedOfficeNetwork(t)
	stale := time.Now().Add(-wireguard.HandshakeStaleAfter - time.Minute).Unix()
	stubWGDump(t, map[string]string{
		wireguard.InterfaceName("office"): ifaceHeader +
			dumpLine("k-laptop", "203.0.113.9:48123", "10.90.12.2/32", stale, 4096, 2048) +
			dumpLine("k-phone", "", "10.90.12.3/32", 0, 0, 0),
	})

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2", len(got))
	}
	if got[0].Connected {
		t.Error("stale handshake must report disconnected")
	}
	if got[0].LastHandshake == nil {
		t.Error("a stale peer still has a last handshake; want it reported, not dropped")
	}
}

// The default/home network is a DNS-only scope with no WireGuard transport. It
// can never have peers, so it must never contribute a row.
func TestListConnectedPeersExcludesDefaultNetwork(t *testing.T) {
	mock := seedOfficeNetwork(t)
	if _, err := mock.Create(&account.Network{
		Name: account.DefaultNetworkName, TLD: "home", Subnet: "10.90.1.0/24",
		Address: "10.90.1.1/24", PublicKey: "HOMEPUB", ListenPort: 51999, Enabled: true,
	}); err != nil {
		t.Fatalf("seed home network: %v", err)
	}
	// A legacy row on home, which must still not surface.
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: account.DefaultNetworkName, PublicKey: "k-legacy",
		Name: "legacy", AllowedIP: "10.90.1.2/32",
	}); err != nil {
		t.Fatalf("add home peer: %v", err)
	}
	stubWGDump(t, map[string]string{
		wireguard.InterfaceName("office"): ifaceHeader,
	})

	for _, p := range callConnectedPeers(t, newNetworksHandler(mock)) {
		if p.Network == account.DefaultNetworkName {
			t.Errorf("home network peer leaked into the panel: %+v", p)
		}
	}
}

// A disabled network has no interface. Its peers must still list (they are
// still enrolled) and `wg` must not be invoked at all — the shell-out would
// only fail and log noise on every poll.
func TestListConnectedPeersDisabledNetworkSkipsDumpButListsPeers(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{
		Name: "off", TLD: "off", Subnet: "10.90.20.0/24",
		Address: "10.90.20.1/24", PublicKey: "PUB", ListenPort: 51830, Enabled: false,
	}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "off", PublicKey: "k-1", Name: "laptop",
		AllowedIP: "10.90.20.2/32", CreatedBy: "alice",
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	called := false
	prev := wgShowDump
	wgShowDump = func(_ context.Context, iface string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { wgShowDump = prev })

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if called {
		t.Error("wg show was invoked for a disabled network")
	}
	if len(got) != 1 {
		t.Fatalf("got %d peers, want 1", len(got))
	}
	if got[0].Connected {
		t.Error("a peer on a disabled network cannot be connected")
	}
}

// One unreadable interface must not blank the panel: the peers still render
// from their persisted rows, just without live state.
func TestListConnectedPeersSurvivesDumpFailure(t *testing.T) {
	mock := seedOfficeNetwork(t)
	prev := wgShowDump
	wgShowDump = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("wg: command not found")
	}
	t.Cleanup(func() { wgShowDump = prev })

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2 despite the dump failing", len(got))
	}
	for _, p := range got {
		if p.Connected {
			t.Errorf("peer %q reports connected with no live data", p.Name)
		}
	}
}

// With no observed endpoint, the configured one is the best available answer.
func TestListConnectedPeersFallsBackToConfiguredEndpoint(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{
		Name: "office", TLD: "office", Subnet: "10.90.12.0/24",
		Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "office", PublicKey: "k-1", Name: "site",
		AllowedIP: "10.90.12.2/32", Endpoint: "branch.example.com:51820",
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	stubWGDump(t, map[string]string{
		wireguard.InterfaceName("office"): ifaceHeader + dumpLine("k-1", "", "10.90.12.2/32", 0, 0, 0),
	})

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if len(got) != 1 {
		t.Fatalf("got %d peers, want 1", len(got))
	}
	if got[0].Endpoint != "branch.example.com:51820" {
		t.Errorf("endpoint = %q, want the configured branch.example.com:51820", got[0].Endpoint)
	}
}

// The observed endpoint is where the peer actually is; the configured one is
// only where we would dial it. Observed must win.
func TestListConnectedPeersObservedEndpointBeatsConfigured(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{
		Name: "office", TLD: "office", Subnet: "10.90.12.0/24",
		Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "office", PublicKey: "k-1", Name: "site",
		AllowedIP: "10.90.12.2/32", Endpoint: "stale.example.com:51820",
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	stubWGDump(t, map[string]string{
		wireguard.InterfaceName("office"): ifaceHeader +
			dumpLine("k-1", "198.51.100.7:33333", "10.90.12.2/32", time.Now().Unix(), 1, 1),
	})

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if got[0].Endpoint != "198.51.100.7:33333" {
		t.Errorf("endpoint = %q, want the observed 198.51.100.7:33333", got[0].Endpoint)
	}
}

// The panel polls; an unstable order would reshuffle rows under the cursor.
func TestListConnectedPeersOrderIsDeterministic(t *testing.T) {
	mock := account.InitMockNetworkManager()
	for _, n := range []string{"zulu", "alpha"} {
		if _, err := mock.Create(&account.Network{
			Name: n, TLD: n, Subnet: "10.90.30.0/24", Address: "10.90.30.1/24",
			PublicKey: "PUB", ListenPort: 51840, Enabled: false,
		}); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
		for _, p := range []string{"yankee", "bravo"} {
			if _, err := mock.AddPeer(&account.NetworkPeer{
				Network: n, PublicKey: "k-" + n + "-" + p, Name: p, AllowedIP: "10.90.30.2/32",
			}); err != nil {
				t.Fatalf("add peer %s/%s: %v", n, p, err)
			}
		}
	}

	want := []string{"alpha/bravo", "alpha/yankee", "zulu/bravo", "zulu/yankee"}
	for range 5 {
		got := callConnectedPeers(t, newNetworksHandler(mock))
		keys := make([]string, 0, len(got))
		for _, p := range got {
			keys = append(keys, p.Network+"/"+p.Name)
		}
		if len(keys) != len(want) {
			t.Fatalf("got %v, want %v", keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("order = %v, want %v", keys, want)
			}
		}
	}
}

// A box with no networks configured must render an empty panel, not an error
// and not a JSON null (which the UI's .map would throw on).
func TestListConnectedPeersEmptyWhenNoNetworks(t *testing.T) {
	got := callConnectedPeers(t, newNetworksHandler(account.InitMockNetworkManager()))
	if len(got) != 0 {
		t.Errorf("got %d peers, want 0", len(got))
	}
}

func TestListConnectedPeersEmptyWhenNoNetworkManager(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &serverBase{}, ctx: t.Context()}
	e := echo.New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/networks/peers/connected", nil)
	rec := httptest.NewRecorder()
	if err := s.listConnectedPeers(e.NewContext(req, rec)); err != nil {
		t.Fatalf("listConnectedPeers: %v", err)
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "[]\n" && body != "[]" {
		t.Errorf("body = %q, want an empty array", body)
	}
}

// The TTL fields drive the panel's expiry column: a WireGuard-only account's
// enrollment lapses, an admin's does not.
func TestListConnectedPeersReportsExpiry(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{
		Name: "office", TLD: "office", Subnet: "10.90.12.0/24",
		Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: false,
	}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "office", PublicKey: "k-scoped", Name: "aaa-scoped",
		AllowedIP: "10.90.12.2/32", CreatedBy: "portal", ExpiresAt: &exp,
	}); err != nil {
		t.Fatalf("add scoped peer: %v", err)
	}
	if _, err := mock.AddPeer(&account.NetworkPeer{
		Network: "office", PublicKey: "k-perm", Name: "bbb-permanent",
		AllowedIP: "10.90.12.3/32", CreatedBy: "admin",
	}); err != nil {
		t.Fatalf("add permanent peer: %v", err)
	}

	got := callConnectedPeers(t, newNetworksHandler(mock))
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2", len(got))
	}
	if got[0].ExpiresAt == nil {
		t.Error("scoped peer expiry is nil, want a timestamp")
	} else if !got[0].ExpiresAt.Equal(exp) {
		t.Errorf("scoped peer expiry = %v, want %v", got[0].ExpiresAt, exp)
	}
	if got[1].ExpiresAt != nil {
		t.Errorf("permanent peer expiry = %v, want nil (never expires)", got[1].ExpiresAt)
	}
}
