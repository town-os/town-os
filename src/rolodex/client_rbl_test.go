package rolodex

import (
	"context"
	"errors"
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
	if err := m.SetRblConfig(ctx, true, providers, 0); err != nil {
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
	if err := m.SetDnsblConfig(ctx, true, providers, 0); err != nil {
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

func TestMockClientDnsblAllowlistEntries(t *testing.T) {
	m := &MockClient{}
	ctx := context.Background()

	if err := m.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{Name: "cdn.example.com", Reason: "false positive"}); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}
	if err := m.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{Name: "mail.example.net", Reason: "needed"}); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}
	// Re-adding the same name updates the reason rather than duplicating,
	// matching rolodex's INSERT OR REPLACE.
	if err := m.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{Name: "cdn.example.com", Reason: "updated"}); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry (update): %v", err)
	}

	entries, err := m.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "cdn.example.com" || entries[0].Reason != "updated" {
		t.Fatalf("expected the re-add to update the reason, got %+v", entries[0])
	}

	if err := m.RemoveDnsblAllowlistEntry(ctx, "cdn.example.com"); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}
	entries, err = m.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "mail.example.net" {
		t.Fatalf("unexpected entries after remove: %+v", entries)
	}
}

// The allowlist and the local blocklist are separate stores: an entry in one
// must never appear in, or be removable through, the other.
func TestMockClientDnsblAllowlistIsSeparateFromLocalRbl(t *testing.T) {
	m := &MockClient{}
	ctx := context.Background()

	if err := m.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: "shared.example.com", Reason: "blocked"}); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}
	if err := m.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{Name: "shared.example.com", Reason: "allowed"}); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}

	if err := m.RemoveDnsblAllowlistEntry(ctx, "shared.example.com"); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}

	blocked, err := m.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(blocked) != 1 {
		t.Fatalf("removing the allowlist entry must not touch the blocklist, got %+v", blocked)
	}
}

func TestMockClientDnsblAllowlistErrorInjection(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("boom")

	m := &MockClient{AddDnsblAllowlistEntryErr: sentinel}
	if err := m.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{Name: "a.example.com"}); !errors.Is(err, sentinel) {
		t.Fatalf("AddDnsblAllowlistEntry = %v, want %v", err, sentinel)
	}

	m = &MockClient{RemoveDnsblAllowlistEntryErr: sentinel}
	if err := m.RemoveDnsblAllowlistEntry(ctx, "a.example.com"); !errors.Is(err, sentinel) {
		t.Fatalf("RemoveDnsblAllowlistEntry = %v, want %v", err, sentinel)
	}

	m = &MockClient{ListDnsblAllowlistEntriesErr: sentinel}
	if _, err := m.ListDnsblAllowlistEntries(ctx); !errors.Is(err, sentinel) {
		t.Fatalf("ListDnsblAllowlistEntries = %v, want %v", err, sentinel)
	}
}

func TestMockClientSatisfiesClientInterface(t *testing.T) {
	var _ Client = &MockClient{}
}
