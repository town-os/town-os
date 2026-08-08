// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
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


// --- DNSBL allowlist (mock rolodex) ---

func TestDNSDnsblAllowlistEntries(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddDnsblAllowlistEntry(ctx, "cdn.example.com", "false positive"); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}
	if err := c.AddDnsblAllowlistEntry(ctx, "mail.example.net", ""); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry (no reason): %v", err)
	}

	entries, err := c.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "cdn.example.com" || entries[0].Reason != "false positive" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}

	if err := c.RemoveDnsblAllowlistEntry(ctx, "cdn.example.com"); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}
	entries, err = c.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "mail.example.net" {
		t.Fatalf("unexpected entries after remove: %+v", entries)
	}
}

// The handler lowercases and trims before storing, so a name typed with stray
// case or whitespace is the same entry as the canonical one — otherwise a
// removal by the displayed name would silently miss.
func TestDNSDnsblAllowlistEntryNormalized(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddDnsblAllowlistEntry(ctx, "  CDN.Example.COM  ", "mixed case"); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}

	entries, err := c.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "cdn.example.com" {
		t.Fatalf("expected the name normalized to cdn.example.com, got %+v", entries)
	}

	if err := c.RemoveDnsblAllowlistEntry(ctx, "CDN.EXAMPLE.COM"); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}
	entries, err = c.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the normalized remove to match, got %+v", entries)
	}
}

func TestDNSDnsblAllowlistEntryInvalidName(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddDnsblAllowlistEntry(ctx, "not a domain", ""); err == nil {
		t.Fatal("expected error for invalid allowlist entry name")
	}
	if err := c.AddDnsblAllowlistEntry(ctx, "singlelabel", ""); err == nil {
		t.Fatal("expected error for a single-label allowlist entry name")
	}
	// An IP literal is valid for the local blocklist but never for the
	// allowlist, which matches names and their subdomains only.
	if err := c.AddDnsblAllowlistEntry(ctx, "192.0.2.10", ""); err == nil {
		t.Fatal("expected error for an IP allowlist entry name")
	}
	if err := c.RemoveDnsblAllowlistEntry(ctx, ""); err == nil {
		t.Fatal("expected error for an empty allowlist entry name")
	}
}

// Rolodex stores an allowlist name fully-qualified and returns it that way. The
// handler presents the bare form, because the local blocklist beside it stores
// names verbatim and the two tables must not render the same name differently.
// The mock does no normalizing of its own, so the dotted entry is seeded
// directly — this asserts the handler's transform, not the mock's.
func TestDNSDnsblAllowlistStripsTrailingDot(t *testing.T) {
	t.Parallel()
	c, mock := initDNSMockTest(t)
	ctx := context.Background()

	mock.DnsblAllowlistEntries = []*upstream.DnsblAllowlistEntry{
		{Name: "cdn.example.com.", Reason: "false positive"},
	}

	entries, err := c.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "cdn.example.com" {
		t.Fatalf("expected the fully-qualified name presented bare, got %+v", entries)
	}
	if entries[0].Reason != "false positive" {
		t.Fatalf("expected the reason untouched, got %q", entries[0].Reason)
	}
}

// A name typed with a trailing dot is the same entry as one without, on the way
// in as well as out — otherwise an operator copying a name out of `dig` output
// would create a second entry that looks identical to the first.
func TestDNSDnsblAllowlistAcceptsTrailingDot(t *testing.T) {
	t.Parallel()
	c, mock := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddDnsblAllowlistEntry(ctx, "cdn.example.com.", "from dig"); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}
	if err := c.AddDnsblAllowlistEntry(ctx, "cdn.example.com", "typed"); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry (bare): %v", err)
	}

	if len(mock.DnsblAllowlistEntries) != 1 {
		t.Fatalf("expected both spellings to be one entry, got %+v", mock.DnsblAllowlistEntries)
	}
	if mock.DnsblAllowlistEntries[0].Name != "cdn.example.com" {
		t.Fatalf("expected the bare name stored, got %q", mock.DnsblAllowlistEntries[0].Name)
	}

	if err := c.RemoveDnsblAllowlistEntry(ctx, "cdn.example.com."); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}
	if len(mock.DnsblAllowlistEntries) != 0 {
		t.Fatalf("expected the dotted spelling to remove the bare entry, got %+v", mock.DnsblAllowlistEntries)
	}
}
