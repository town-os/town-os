// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// orderedSystemd wraps the systemd mock to observe the instant the WireGuard
// interface unit is started — the instant the overlay address comes into
// existence on the host.
type orderedSystemd struct {
	*systemd.MockManager

	mu          sync.Mutex
	started     bool
	dnsWasEarly bool
	observe     func() bool // reports whether rolodex has been told to listen yet
}

func (o *orderedSystemd) SetStatus(ctx context.Context, unit string, action systemd.StatusAction) error {
	o.mu.Lock()
	if action == systemd.Restart {
		o.started = true
		// If rolodex has already been pointed at the overlay address, it was
		// pointed there before the interface existed.
		if o.observe() {
			o.dnsWasEarly = true
		}
	}
	o.mu.Unlock()
	return o.MockManager.SetStatus(ctx, unit, action)
}

// TestOverlayDNSListenerIsProgrammedAfterInterfaceStarts pins the ordering that
// decides whether a WireGuard peer can resolve anything at all.
//
// rolodex binds a DNS listener on the box's overlay address (10.x.y.1) because
// that is the resolver every peer config points at (`DNS = <overlay .1>`). A bind
// against an address the host does not have yet fails with EADDRNOTAVAIL and the
// listener task dies — and rolodex, which records the listener at spawn time,
// then believes the address is served while nothing is listening on it. Every
// later re-assert is a no-op against that dead entry, so the tunnel comes up and
// every name looked up over it times out, permanently.
//
// The controller used to program rolodex at the TOP of applyNetworkTransport,
// before it wrote the config, installed the unit, or started the interface — so
// it asked rolodex to bind an address it had not created yet. This asserts the
// inverse: nothing is pointed at the overlay address until systemd has been told
// to bring the interface up.
func TestOverlayDNSListenerIsProgrammedAfterInterfaceStarts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	nm := initNetworkDB(t)
	mc := &rolodex.MockClient{}
	sd := &orderedSystemd{
		MockManager: systemd.InitMockManager(),
		observe:     func() bool { return len(mc.ScopeTldListeners) > 0 },
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		RolodexClient:    mc,
		NetworkStatePath: t.TempDir(),
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := c.CreateNetwork(ctx, "office", "office"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	sd.mu.Lock()
	started, early := sd.started, sd.dnsWasEarly
	sd.mu.Unlock()

	if !started {
		t.Fatal("the network interface unit was never started")
	}
	if early {
		t.Error("rolodex was pointed at the overlay address before the interface was started: " +
			"the bind fails EADDRNOTAVAIL and the listener never recovers")
	}

	// And it IS programmed by the end — on the overlay address, under the
	// network's TLD. Without this the peer's `DNS = 10.x.y.1` reaches a closed
	// port.
	if len(mc.ScopeTldListeners) == 0 {
		t.Fatal("no overlay DNS listener was programmed; peers would have no resolver")
	}
	listenIP, ok := mc.ScopeTldListeners["office\x00office."]
	if !ok {
		t.Fatalf("no listener for the office TLD: %+v", mc.ScopeTldListeners)
	}
	nets, err := c.ListNetworks(ctx)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(nets) != 1 {
		t.Fatalf("expected 1 network, got %d", len(nets))
	}
	// The listener address is the box's own overlay address (.1 of the subnet),
	// which is exactly what renderPeerDeviceConfig hands peers as their DNS.
	wantIP := nets[0].Address
	if idx := len(wantIP) - len("/24"); idx > 0 && wantIP[idx:] == "/24" {
		wantIP = wantIP[:idx]
	}
	if listenIP != wantIP {
		t.Errorf("listener bound to %q, want the box's overlay address %q", listenIP, wantIP)
	}
}
