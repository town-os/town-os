package rolodex

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

func scopedRecord(recs []*upstream.DnsRecord, name string, rt upstream.RecordType) *upstream.DnsRecord {
	for _, r := range recs {
		if r.Name == name && r.RecordType == rt {
			return r
		}
	}
	return nil
}

// EnsureScopedTLD publishes the zone apex (SOA, NS, ns1 A) for a network-owned
// TLD as records scoped to that network.
func TestEnsureScopedTLDPublishesApex(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	if err := EnsureScopedTLD(ctx, mc, "office", "office", "10.90.12.1", ""); err != nil {
		t.Fatalf("EnsureScopedTLD: %v", err)
	}

	recs := mc.ScopedRecords["office"]
	if len(recs) != 3 {
		t.Fatalf("expected 3 scoped apex records, got %d: %+v", len(recs), recs)
	}

	soa := scopedRecord(recs, "office.", upstream.RecordTypeSOA)
	if soa == nil {
		t.Fatalf("missing scoped SOA at office. in %+v", recs)
	}

	ns := scopedRecord(recs, "office.", upstream.RecordTypeNS)
	if ns == nil || ns.Value != "ns1.office." {
		t.Fatalf("missing/incorrect scoped NS: %+v", ns)
	}

	a := scopedRecord(recs, "ns1.office.", upstream.RecordTypeA)
	if a == nil || a.Value != "10.90.12.1" {
		t.Fatalf("missing/incorrect scoped ns1 A: %+v", a)
	}

	// Nothing must leak into the global zone — the TLD is owned by the scope.
	if len(mc.Records) != 0 {
		t.Fatalf("scoped TLD setup must not touch the global zone, got %+v", mc.Records)
	}
}

// EnsureScopedTLD is a no-op when the apex SOA already exists, so it is safe to
// call on every reconcile.
func TestEnsureScopedTLDIdempotent(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	if err := EnsureScopedTLD(ctx, mc, "office", "office", "10.90.12.1", ""); err != nil {
		t.Fatalf("EnsureScopedTLD: %v", err)
	}
	if err := EnsureScopedTLD(ctx, mc, "office", "office", "10.90.12.1", ""); err != nil {
		t.Fatalf("EnsureScopedTLD (second): %v", err)
	}

	if got := len(mc.ScopedRecords["office"]); got != 3 {
		t.Fatalf("expected apex published once (3 records), got %d", got)
	}

	var adds int
	for _, c := range mc.GetCalls() {
		if c.Method == "AddScopedRecord" {
			adds++
		}
	}
	if adds != 3 {
		t.Fatalf("expected exactly 3 AddScopedRecord calls across both runs, got %d", adds)
	}
}

// An empty ipv4/ipv6 skips the ns1 address record but still publishes SOA + NS.
func TestEnsureScopedTLDNoAddress(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	if err := EnsureScopedTLD(ctx, mc, "lab", "lab", "", ""); err != nil {
		t.Fatalf("EnsureScopedTLD: %v", err)
	}

	recs := mc.ScopedRecords["lab"]
	if len(recs) != 2 {
		t.Fatalf("expected SOA + NS only, got %d: %+v", len(recs), recs)
	}
	if scopedRecord(recs, "ns1.lab.", upstream.RecordTypeA) != nil {
		t.Fatal("no ns1 A record should be published when ipv4 is empty")
	}
}
