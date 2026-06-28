package rolodex

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

func TestMockClientRblConfig(t *testing.T) {
	m := &MockClient{}
	ctx := context.Background()

	providers := []*upstream.RblConfig{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "bl.spamcop.net", Enabled: false},
	}
	if err := m.SetRblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	status, err := m.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if !status.Enabled {
		t.Fatal("expected RBL enabled")
	}
	if len(status.Providers) != 2 || status.Providers[0].Zone != "zen.spamhaus.org" {
		t.Fatalf("unexpected providers: %+v", status.Providers)
	}
}

func TestMockClientDnsblConfig(t *testing.T) {
	m := &MockClient{}
	ctx := context.Background()

	providers := []*upstream.DnsblConfig{{Zone: "dbl.spamhaus.org", Enabled: true}}
	if err := m.SetDnsblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	status, err := m.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !status.Enabled || len(status.Providers) != 1 || status.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Fatalf("unexpected DNSBL status: %+v", status)
	}
}

func TestMockClientLocalRblEntries(t *testing.T) {
	m := &MockClient{}
	ctx := context.Background()

	if err := m.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: "ads.example.com", Reason: "ads"}); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}
	if err := m.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: "tracker.example.net", Reason: "tracker"}); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}
	// Re-adding the same name updates the reason rather than duplicating.
	if err := m.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: "ads.example.com", Reason: "updated"}); err != nil {
		t.Fatalf("AddLocalRblEntry (update): %v", err)
	}

	entries, err := m.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if err := m.RemoveLocalRblEntry(ctx, "ads.example.com"); err != nil {
		t.Fatalf("RemoveLocalRblEntry: %v", err)
	}
	entries, err = m.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "tracker.example.net" {
		t.Fatalf("unexpected entries after remove: %+v", entries)
	}
}

func TestMockClientSatisfiesClientInterface(t *testing.T) {
	var _ Client = &MockClient{}
}
