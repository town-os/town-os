// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/systemd"
)

func scopedApex(recs []*upstream.DnsRecord, name string, rt upstream.RecordType) *upstream.DnsRecord {
	for _, r := range recs {
		if r.Name == name && r.RecordType == rt {
			return r
		}
	}
	return nil
}

// Creating a non-default network publishes the network TLD's zone apex (SOA, NS,
// ns1 A pointing at the host's overlay IP) scoped to the network, so the owned
// zone is authoritative and resolvable on the overlay.
func TestApplyNetworkTransportPublishesScopedTLDApex(t *testing.T) {
	mock := account.InitMockNetworkManager()
	n := &account.Network{Name: "office", TLD: "office", Subnet: "10.90.12.0/24", Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	if _, err := mock.Create(t.Context(), n); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	mc := &rolodex.MockClient{}
	s := newNetworksHandlerWithRolodex(mock, mc)

	if err := s.applyNetworkTransport(context.Background(), n); err != nil {
		t.Fatalf("applyNetworkTransport: %v", err)
	}

	recs := mc.ScopedRecords["office"]
	if scopedApex(recs, "office.", upstream.RecordTypeSOA) == nil {
		t.Fatalf("missing scoped SOA at office. in %+v", recs)
	}
	if ns := scopedApex(recs, "office.", upstream.RecordTypeNS); ns == nil || ns.Value != "ns1.office." {
		t.Fatalf("missing/incorrect scoped NS: %+v", ns)
	}
	if a := scopedApex(recs, "ns1.office.", upstream.RecordTypeA); a == nil || a.Value != "10.90.12.1" {
		t.Fatalf("ns1 A must point at the host overlay IP 10.90.12.1, got %+v", a)
	}
	// The owned TLD apex must never leak into the global zone.
	if len(mc.Records) != 0 {
		t.Fatalf("scoped TLD apex must not touch the global zone, got %+v", mc.Records)
	}
}

// An enabled non-default network must bind a rolodex DNS listener on the box's
// overlay address. The peer configs we hand out set `DNS = <overlay .1>`
// (renderPeerDeviceConfig), and rolodex otherwise binds only loopback and the
// default-route interface — so without this a peer's DNS query lands on a closed
// port and the overlay has no resolver at all.
func TestApplyNetworkTransportBindsOverlayDNSListener(t *testing.T) {
	mock := account.InitMockNetworkManager()
	n := &account.Network{Name: "office", TLD: "office", Subnet: "10.90.12.0/24", Address: "10.90.12.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	if _, err := mock.Create(t.Context(), n); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	mc := &rolodex.MockClient{}
	s := newNetworksHandlerWithRolodex(mock, mc)

	if err := s.applyNetworkTransport(context.Background(), n); err != nil {
		t.Fatalf("applyNetworkTransport: %v", err)
	}

	got := mc.ScopeTldListeners["office\x00office."]
	if got != "10.90.12.1" {
		t.Fatalf("expected a DNS listener bound to the overlay address 10.90.12.1 for office., got %q (listeners: %+v)", got, mc.ScopeTldListeners)
	}
}

// The default/home network has no WireGuard transport and no overlay address, so
// there is nothing to listen on — it must not bind an ingress listener.
func TestApplyNetworkTransportDefaultNetworkNoOverlayDNSListener(t *testing.T) {
	mock := account.InitMockNetworkManager()
	n := &account.Network{Name: account.DefaultNetworkName, TLD: "home", Subnet: "10.64.0.0/24", Address: "10.64.0.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	mock.Seed(n)
	mc := &rolodex.MockClient{}
	s := newNetworksHandlerWithRolodex(mock, mc)

	if err := s.applyNetworkTransport(context.Background(), n); err != nil {
		t.Fatalf("applyNetworkTransport: %v", err)
	}

	if len(mc.ScopeTldListeners) != 0 {
		t.Fatalf("default network must not bind an overlay DNS listener, got %+v", mc.ScopeTldListeners)
	}
}

// The default network's home zone is global (set up by SetupDNS), so
// applyNetworkTransport must not publish a scoped apex for it.
func TestApplyNetworkTransportDefaultNetworkNoScopedApex(t *testing.T) {
	mock := account.InitMockNetworkManager()
	n := &account.Network{Name: account.DefaultNetworkName, TLD: "home", Subnet: "10.64.0.0/24", Address: "10.64.0.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	mock.Seed(n)
	mc := &rolodex.MockClient{}
	s := newNetworksHandlerWithRolodex(mock, mc)

	if err := s.applyNetworkTransport(context.Background(), n); err != nil {
		t.Fatalf("applyNetworkTransport: %v", err)
	}

	if len(mc.ScopedRecords[account.DefaultNetworkName]) != 0 {
		t.Fatalf("default network must not publish a scoped TLD apex, got %+v", mc.ScopedRecords)
	}
}

// The default/home network is a DNS-only owned scope: applyNetworkTransport must
// create its rolodex scope (so .home is owned and therefore hidden from WireGuard
// peers) but install NO WireGuard systemd unit and bind NO overlay association —
// .home has no WireGuard transport of its own and stays LAN-only.
func TestApplyNetworkTransportDefaultNetworkNoWireGuardTransport(t *testing.T) {
	mock := account.InitMockNetworkManager()
	n := &account.Network{Name: account.DefaultNetworkName, TLD: "home", Subnet: "10.64.0.0/24", Address: "10.64.0.1/24", PublicKey: "PUB", PrivateKey: "PRIV", ListenPort: 51820, Enabled: true}
	mock.Seed(n)
	mc := &rolodex.MockClient{}
	sd := systemd.InitMockManager()
	sb := &serverBase{ServerConfig: ServerConfig{
		NetworkMgr:       mock,
		RolodexClient:    mc,
		Systemd:          sd,
		NetworkStatePath: t.TempDir(),
	}}
	s := &SystemControllerHandlers{Controller: sb, ctx: context.Background()}

	if err := s.applyNetworkTransport(context.Background(), n); err != nil {
		t.Fatalf("applyNetworkTransport: %v", err)
	}

	// The home scope is created and owns the home TLD via its home_domain.
	found := false
	for _, sc := range mc.Scopes {
		if sc.Name == account.DefaultNetworkName && sc.HomeDomain == "home." {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a rolodex scope %q owning home., got %+v", account.DefaultNetworkName, mc.Scopes)
	}

	// No WireGuard systemd unit is installed for the default network.
	if _, ok := sd.InstalledUnits[systemd.NetworkUnitName(account.DefaultNetworkName)]; ok {
		t.Fatalf("default network must not install a WireGuard unit, got %v", sd.InstalledUnits)
	}

	// No overlay association is bound: nothing joins the home scope, so no source
	// IP resolves .home over WireGuard — it is reachable only from the LAN.
	if len(mc.Associations) != 0 {
		t.Fatalf("default network must not bind an overlay association, got %+v", mc.Associations)
	}
}

// networkOverlayIPValue feeds the leaf's overlay SAN, so a WireGuard peer can
// reach a package by raw overlay address. The default network is DNS-only (no
// WireGuard transport at all), so it must yield "" — an empty SAN is skipped by
// collectTLSSans, which is what stops default-network leaves from churning on
// every reconcile.
func TestNetworkOverlayIPValue(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(t.Context(), &account.Network{
		Name: "fart", TLD: "fart", Subnet: "10.65.0.0/24", Address: "10.65.0.1/24",
		PublicKey: "PUB", ListenPort: 51820, Enabled: true,
	}); err != nil {
		t.Fatalf("create network: %v", err)
	}

	if got := networkOverlayIPValue(t.Context(), nm, "fart"); got != "10.65.0.1" {
		t.Errorf("networkOverlayIPValue(fart) = %q, want 10.65.0.1", got)
	}
	if got := networkOverlayIPValue(t.Context(), nm, account.DefaultNetworkName); got != "" {
		t.Errorf("default network has no WireGuard transport; want \"\", got %q", got)
	}
	if got := networkOverlayIPValue(t.Context(), nm, ""); got != "" {
		t.Errorf("empty network: want \"\", got %q", got)
	}
	if got := networkOverlayIPValue(t.Context(), nm, "nonexistent"); got != "" {
		t.Errorf("unknown network: want \"\", got %q", got)
	}
	if got := networkOverlayIPValue(t.Context(), nil, "fart"); got != "" {
		t.Errorf("nil manager: want \"\", got %q", got)
	}
}
