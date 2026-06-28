// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- RBL / DNSBL config (mock rolodex) ---

func TestDNSRblConfigRoundtrip(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "bl.spamcop.net", Enabled: false},
	}
	if err := c.SetRblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	got, err := c.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if !got.Enabled || len(got.Providers) != 2 || got.Providers[0].Zone != "zen.spamhaus.org" {
		t.Fatalf("unexpected RBL config: %+v", got)
	}
}

func TestDNSDnsblConfigRoundtrip(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true}}
	if err := c.SetDnsblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	got, err := c.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !got.Enabled || len(got.Providers) != 1 || got.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Fatalf("unexpected DNSBL config: %+v", got)
	}
}

func TestDNSRblConfigInvalidZone(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{{Zone: "not_a_zone", Enabled: true}})
	if err == nil {
		t.Fatal("expected error for invalid zone")
	}
}

func TestDNSRblConfigDuplicateZone(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "zen.spamhaus.org", Enabled: false},
	})
	if err == nil {
		t.Fatal("expected error for duplicate zone")
	}
}

// --- Local RBL entries (mock rolodex) ---

func TestDNSLocalRblEntries(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddLocalRblEntry(ctx, "ads.example.com", "ad tracker"); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}
	if err := c.AddLocalRblEntry(ctx, "192.0.2.10", "bad ip"); err != nil {
		t.Fatalf("AddLocalRblEntry(ip): %v", err)
	}

	entries, err := c.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if err := c.RemoveLocalRblEntry(ctx, "ads.example.com"); err != nil {
		t.Fatalf("RemoveLocalRblEntry: %v", err)
	}
	entries, err = c.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "192.0.2.10" {
		t.Fatalf("unexpected entries after remove: %+v", entries)
	}
}

func TestDNSLocalRblEntryInvalidName(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddLocalRblEntry(ctx, "not a domain", ""); err == nil {
		t.Fatal("expected error for invalid local RBL entry name")
	}
}

