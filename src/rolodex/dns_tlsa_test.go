// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

func TestRegisterPackageTLSA(t *testing.T) {
	mc := &MockClient{}
	entries := []TLSAEntry{
		{Name: "gitea.default.home", Port: 443, Value: "3 1 1 abc"},
		{Name: "git.gitea.default.home", Port: 443, Value: "3 1 1 abc"},
		{Name: "skip.me", Port: 80, Value: ""}, // empty value → skipped
	}
	if err := RegisterPackageTLSA(context.Background(), mc, entries); err != nil {
		t.Fatalf("RegisterPackageTLSA: %v", err)
	}

	if len(mc.Records) != 2 {
		t.Fatalf("expected 2 TLSA records (empty value skipped), got %d: %+v", len(mc.Records), mc.Records)
	}
	r := mc.Records[0]
	if r.RecordType != upstream.RecordTypeTLSA {
		t.Errorf("record type = %v, want TLSA", r.RecordType)
	}
	if r.Name != "_443._tcp.gitea.default.home." {
		t.Errorf("owner name = %q, want RFC 6698 _443._tcp.gitea.default.home.", r.Name)
	}
	if r.Value != "3 1 1 abc" {
		t.Errorf("value = %q", r.Value)
	}
	if r.Ttl != 300 {
		t.Errorf("ttl = %d, want 300", r.Ttl)
	}
}

func TestUnregisterPackageTLSA(t *testing.T) {
	mc := &MockClient{
		Records: []*upstream.DnsRecord{
			{Name: "_443._tcp.gitea.default.home.", RecordType: upstream.RecordTypeTLSA, Value: "3 1 1 abc"},
		},
	}
	entries := []TLSAEntry{{Name: "gitea.default.home", Port: 443}}
	if err := UnregisterPackageTLSA(context.Background(), mc, entries); err != nil {
		t.Fatalf("UnregisterPackageTLSA: %v", err)
	}
	if len(mc.Records) != 0 {
		t.Fatalf("expected TLSA record removed, got %+v", mc.Records)
	}
	// Confirm removal was keyed by the RFC 6698 owner name + TLSA type.
	calls := mc.GetCalls()
	last := calls[len(calls)-1]
	name, ok := last.Args[0].(string)
	if last.Method != "RemoveRecord" || !ok || name != "_443._tcp.gitea.default.home." {
		t.Fatalf("unexpected remove call: %+v", last)
	}
}

// TestUnregisterScopedPackageTLSA pins the inverse of
// RegisterScopedPackageTLSA. The scoped pin lives at the _<port>._tcp owner
// name, NOT at the host name, so removing a host's scoped address records
// leaves it untouched — which is why this function has to exist at all.
func TestUnregisterScopedPackageTLSA(t *testing.T) {
	mc := &MockClient{
		ScopedRecords: map[string][]*upstream.DnsRecord{
			"fart": {
				{Name: "secret.fart.", RecordType: upstream.RecordTypeA, Value: "10.65.0.1"},
				{Name: "_443._tcp.secret.fart.", RecordType: upstream.RecordTypeTLSA, Value: "3 1 1 abc"},
			},
		},
	}

	// Removing the host's scoped records does NOT reach the pin.
	if _, err := mc.RemoveScopedRecord(context.Background(), "fart", "secret.fart.", nil); err != nil {
		t.Fatalf("RemoveScopedRecord: %v", err)
	}
	if len(mc.ScopedRecords["fart"]) != 1 {
		t.Fatalf("expected the TLSA pin to survive an address-record removal, got %+v", mc.ScopedRecords["fart"])
	}

	entries := []TLSAEntry{{Name: "secret.fart", Port: 443}}
	if err := UnregisterScopedPackageTLSA(context.Background(), mc, "fart", entries); err != nil {
		t.Fatalf("UnregisterScopedPackageTLSA: %v", err)
	}
	if len(mc.ScopedRecords["fart"]) != 0 {
		t.Fatalf("expected the scoped TLSA pin removed, got %+v", mc.ScopedRecords["fart"])
	}

	calls := mc.GetCalls()
	last := calls[len(calls)-1]
	scope, scopeOK := last.Args[0].(string)
	name, nameOK := last.Args[1].(string)
	if last.Method != "RemoveScopedRecord" || !scopeOK || !nameOK ||
		scope != "fart" || name != "_443._tcp.secret.fart." {
		t.Fatalf("unexpected remove call: %+v", last)
	}
}

func TestTLSANameFormat(t *testing.T) {
	if got := tlsaName("app.repo.home", 8443); got != "_8443._tcp.app.repo.home." {
		t.Fatalf("tlsaName = %q", got)
	}
	// Trailing dot on the input must not double up.
	if got := tlsaName("app.repo.home.", 443); got != "_443._tcp.app.repo.home." {
		t.Fatalf("tlsaName with trailing dot = %q", got)
	}
}
