// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net/netip"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initNetworkRolodexHTTPTest builds a test server backed by a real sqlite
// network manager, a mock systemd manager, a real network-state directory, and
// a rolodex mock, so tests can drive /networks* end-to-end and assert on the
// DNS records the create path publishes.
func initNetworkRolodexHTTPTest(t *testing.T) (*systemcontroller.SystemdClient, *rolodex.MockClient) {
	t.Helper()

	nm := initNetworkDB(t)
	sd := systemd.InitMockManager()
	stateDir := t.TempDir()
	rc := &rolodex.MockClient{}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		NetworkMgr:       nm,
		Systemd:          sd,
		NetworkStatePath: stateDir,
		RolodexClient:    rc,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}
	return c, rc
}

func findScopedRecord(recs []*upstream.DnsRecord, name string, rt upstream.RecordType) *upstream.DnsRecord {
	for _, r := range recs {
		if r.Name == name && r.RecordType == rt {
			return r
		}
	}
	return nil
}

// overlayAddr extracts the host address from a CIDR (e.g. "10.90.12.1/24" ->
// "10.90.12.1"), matching how the controller derives ns1's address.
func overlayAddr(t *testing.T, cidr string) string {
	t.Helper()
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		t.Fatalf("parse address %q: %v", cidr, err)
	}
	return p.Addr().String()
}

// assertFartApex asserts the fart scope carries the zone apex (SOA + NS + ns1 A
// pointing at wantNS1) and nothing leaked into the global zone.
func assertFartApex(t *testing.T, rc *rolodex.MockClient, wantNS1 string) {
	t.Helper()
	recs := rc.ScopedRecords["fart"]
	if findScopedRecord(recs, "fart.", upstream.RecordTypeSOA) == nil {
		t.Fatalf("missing scoped SOA at fart. in %+v", recs)
	}
	if ns := findScopedRecord(recs, "fart.", upstream.RecordTypeNS); ns == nil || ns.Value != "ns1.fart." {
		t.Fatalf("missing/incorrect scoped NS: %+v", ns)
	}
	if a := findScopedRecord(recs, "ns1.fart.", upstream.RecordTypeA); a == nil || a.Value != wantNS1 {
		t.Fatalf("ns1.fart. A must point at the overlay IP %q, got %+v", wantNS1, a)
	}
	if len(rc.Records) != 0 {
		t.Fatalf("the owned TLD apex must not leak into the global zone, got %+v", rc.Records)
	}
}

// TestCreateNetworkPublishesScopedTLDApex drives POST /networks/create end-to-end
// through the router, the real sqlite network manager, and applyNetworkTransport,
// and asserts the network TLD's zone apex (SOA/NS/ns1) is published scoped to the
// network — the records a fresh network's TLD needs to resolve on the overlay.
func TestCreateNetworkPublishesScopedTLDApex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, rc := initNetworkRolodexHTTPTest(t)

	view, err := c.CreateNetwork(ctx, "fart", "fart")
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// The scope was created with the network TLD as its home domain.
	if len(rc.Scopes) != 1 || rc.Scopes[0].Name != "fart" || rc.Scopes[0].HomeDomain != "fart." {
		t.Fatalf("unexpected scopes: %+v", rc.Scopes)
	}
	assertFartApex(t, rc, overlayAddr(t, view.Address))
}

// TestDeleteAndRecreateNetworkRepublishesTLDApex reproduces the operator flow of
// deleting a network and recreating it: the recreated network must get its TLD
// apex back (delete clears the scope + its records; recreate republishes them),
// so the .fart TLD resolves again after a delete/recreate cycle.
func TestDeleteAndRecreateNetworkRepublishesTLDApex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, rc := initNetworkRolodexHTTPTest(t)

	if _, err := c.CreateNetwork(ctx, "fart", "fart"); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if findScopedRecord(rc.ScopedRecords["fart"], "fart.", upstream.RecordTypeSOA) == nil {
		t.Fatal("apex missing after first create")
	}

	// Delete → the scope and its records are torn down.
	if err := c.RemoveNetwork(ctx, "fart"); err != nil {
		t.Fatalf("RemoveNetwork: %v", err)
	}
	if len(rc.ScopedRecords["fart"]) != 0 {
		t.Fatalf("delete must clear the scope's records, got %+v", rc.ScopedRecords["fart"])
	}

	// Recreate → the apex must be republished, not silently skipped.
	view, err := c.CreateNetwork(ctx, "fart", "fart")
	if err != nil {
		t.Fatalf("recreate CreateNetwork: %v", err)
	}
	assertFartApex(t, rc, overlayAddr(t, view.Address))
}
