// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// addPeerWithHost posts to /networks/peers/add over real HTTP with an explicit
// Host header — i.e. as a device that reached the box at that address — and
// returns the enrollment result.
func addPeerWithHost(t *testing.T, baseURL, host, network, name string) systemcontroller.AddPeerResult {
	t.Helper()

	body, err := json.Marshal(systemcontroller.AddNetworkPeerRequest{Network: network, Name: name})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/networks/peers/add", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The Host header is what the client dialed; the connection still goes to the
	// test server's loopback address, exactly as a socat/NAT relay would deliver
	// it.
	if host != "" {
		req.Host = host
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add peer: status %d", resp.StatusCode)
	}

	var res systemcontroller.AddPeerResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode add peer result: %v", err)
	}
	return res
}

// TestPeerConfigEndpointIsTheAddressTheClientDialed drives enrollment end-to-end
// through the router and asserts the peer config advertises the address the
// device actually reached the box on.
//
// This is the difference between a tunnel that connects and one that silently
// never does. The box used to fill Endpoint from its own view of itself — its
// public IP (ipinfo.io), falling back to its LAN address — and both are
// unroutable from a device behind a NAT, a port forward, or a relay: the phone
// on the same Wi-Fi cannot hairpin to the box's public IP, and cannot route to
// the box's private LAN address at all. The peer then handshakes into a void,
// which on the box looks like a peer with no endpoint, no handshake and zero
// transfer, and on the phone looks like DNS is broken.
//
// The Host header is the one address known to work, because the enrollment
// request itself arrived over it.
func TestPeerConfigEndpointIsTheAddressTheClientDialed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	nm := initNetworkDB(t)
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		NetworkStatePath: t.TempDir(),
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	view, err := c.CreateNetwork(ctx, "office", "office")
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// A phone that reached the box through the host relay at 192.168.8.174:5309.
	res := addPeerWithHost(t, ts.Server.URL, "192.168.8.174:5309", "office", "phone")

	wantEndpoint := fmt.Sprintf("Endpoint = 192.168.8.174:%d", view.ListenPort)
	if !strings.Contains(res.Config, wantEndpoint) {
		t.Errorf("peer config must advertise the dialed address, want %q:\n%s", wantEndpoint, res.Config)
	}
	// The overlay resolver the peer is told to use is the box's own overlay
	// address — the thing rolodex must be listening on.
	if !strings.Contains(res.Config, "DNS = ") {
		t.Errorf("peer config missing DNS line:\n%s", res.Config)
	}

	// A device enrolling from the box itself has no remotely-dialable address to
	// be told about: omit Endpoint rather than advertise loopback, which would
	// hand out a config that cannot work anywhere.
	local := addPeerWithHost(t, ts.Server.URL, "", "office", "onbox")
	if strings.Contains(local.Config, "Endpoint") {
		t.Errorf("loopback enrollment must not advertise an Endpoint:\n%s", local.Config)
	}
}
