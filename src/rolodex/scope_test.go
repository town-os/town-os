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
