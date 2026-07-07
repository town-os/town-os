package rolodex

import (
	"context"
	"testing"
)

func TestEnsureNetworkScopeCreatesOnce(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	if err := EnsureNetworkScope(ctx, mc, "office", "office.home."); err != nil {
		t.Fatalf("EnsureNetworkScope: %v", err)
	}
	if len(mc.Scopes) != 1 || mc.Scopes[0].Name != "office" || mc.Scopes[0].HomeDomain != "office.home." {
		t.Fatalf("unexpected scopes: %+v", mc.Scopes)
	}

	// Second call is a no-op (idempotent).
	if err := EnsureNetworkScope(ctx, mc, "office", "office.home."); err != nil {
		t.Fatalf("EnsureNetworkScope again: %v", err)
	}
	if len(mc.Scopes) != 1 {
		t.Fatalf("expected 1 scope after idempotent call, got %d", len(mc.Scopes))
	}

	var creates int
	for _, c := range mc.GetCalls() {
		if c.Method == "CreateNetworkScope" {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("expected exactly 1 CreateNetworkScope call, got %d", creates)
	}
}

func TestBindOverlayAddress(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	if err := BindOverlayAddress(ctx, mc, "10.90.12.1", "office"); err != nil {
		t.Fatalf("BindOverlayAddress: %v", err)
	}
	assocs, err := mc.GetNetworkAssociations(ctx, "office")
	if err != nil {
		t.Fatalf("GetNetworkAssociations: %v", err)
	}
	if len(assocs) != 1 || assocs[0].IpAddress != "10.90.12.1" || assocs[0].ScopeName != "office" {
		t.Fatalf("unexpected associations: %+v", assocs)
	}
}

func TestEnsureScopeTld(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	if err := EnsureScopeTld(ctx, mc, "office", "corp."); err != nil {
		t.Fatalf("EnsureScopeTld: %v", err)
	}
	if got := mc.ScopeTlds["office"]; len(got) != 1 || got[0] != "corp." {
		t.Fatalf("unexpected scope tlds: %+v", mc.ScopeTlds)
	}

	// Second call is a no-op (idempotent) — no extra AddScopeTld.
	if err := EnsureScopeTld(ctx, mc, "office", "corp."); err != nil {
		t.Fatalf("EnsureScopeTld again: %v", err)
	}
	var adds int
	for _, c := range mc.GetCalls() {
		if c.Method == "AddScopeTld" {
			adds++
		}
	}
	if adds != 1 {
		t.Fatalf("expected exactly 1 AddScopeTld call, got %d", adds)
	}
}

func TestReconcileTldForwarders(t *testing.T) {
	ctx := context.Background()
	mc := &MockClient{}

	addrs := []string{"10.90.12.2:53", "10.90.12.3:53"}
	if err := ReconcileTldForwarders(ctx, mc, "office", "office.", addrs); err != nil {
		t.Fatalf("ReconcileTldForwarders: %v", err)
	}
	got, err := mc.ListScopeTldForwarders(ctx, "office", "office.")
	if err != nil {
		t.Fatalf("ListScopeTldForwarders: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d forwarders, want 2: %v", len(got), got)
	}

	// Replace-all: an empty set clears the forwarders.
	if err := ReconcileTldForwarders(ctx, mc, "office", "office.", nil); err != nil {
		t.Fatalf("ReconcileTldForwarders clear: %v", err)
	}
	got, err = mc.ListScopeTldForwarders(ctx, "office", "office.")
	if err != nil {
		t.Fatalf("ListScopeTldForwarders after clear: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected forwarders cleared, got %v", got)
	}
}
