// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
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
	if _, err := mock.Create(n); err != nil {
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

// The default network's home zone is global (set up by SetupDNS), so
// applyNetworkTransport must not publish a scoped apex for it.
func TestApplyNetworkTransportDefaultNetworkNoScopedApex(t *testing.T) {
	mock := account.InitMockNetworkManager()
	n := &account.Network{Name: account.DefaultNetworkName, TLD: "home", Subnet: "10.64.0.0/24", Address: "10.64.0.1/24", PublicKey: "PUB", ListenPort: 51820, Enabled: true}
	if _, err := mock.Create(n); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	mc := &rolodex.MockClient{}
	s := newNetworksHandlerWithRolodex(mock, mc)

	if err := s.applyNetworkTransport(context.Background(), n); err != nil {
		t.Fatalf("applyNetworkTransport: %v", err)
	}

	if len(mc.ScopedRecords[account.DefaultNetworkName]) != 0 {
		t.Fatalf("default network must not publish a scoped TLD apex, got %+v", mc.ScopedRecords)
	}
}
