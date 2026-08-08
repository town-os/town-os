// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"slices"
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
	if err := c.SetRblConfig(ctx, true, providers, 0); err != nil {
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
	if err := c.SetDnsblConfig(ctx, true, providers, 0); err != nil {
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

	err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{{Zone: "not_a_zone", Enabled: true}}, 0)
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
	}, 0)
	if err == nil {
		t.Fatal("expected error for duplicate zone")
	}
}

// --- Refusal codes (mock rolodex) ---
//
// A DNSxL answers a refusal — "you queried via a public resolver", "you are
// over your query limit" — with an A record in the same 127.0.0.0/8 space it
// answers a listing with, so only the address separates them. Believing a
// refusal NXDOMAINs every name checked against that provider, which is what a
// household box that quietly exceeds Spamhaus's free-use limit runs into. These
// exercise the API that lets an operator say what a refusal looks like for a
// given provider, and see which providers are currently backed off because of
// one.

// TestDNSRblRefusalCodesRoundtrip proves per-provider codes and cooldowns
// survive the full HTTP path, including being masked to their prefix.
func TestDNSRblRefusalCodesRoundtrip(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{
		{
			Zone:                "zen.spamhaus.org",
			Enabled:             true,
			RefusalCodes:        []string{"127.255.255.254/24", "127.0.0.1"},
			RefusalCooldownSecs: 900,
		},
		{Zone: "bl.spamcop.net", Enabled: true},
	}
	if err := c.SetRblConfig(ctx, true, providers, 1800); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	got, err := c.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if got.RefusalCooldownSecs != 1800 {
		t.Errorf("list-wide cooldown = %d, want 1800", got.RefusalCooldownSecs)
	}
	if len(got.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %+v", got.Providers)
	}
	if !slices.Equal(got.Providers[0].RefusalCodes, []string{"127.255.255.0/24", "127.0.0.1"}) {
		t.Errorf("refusal codes = %v, want them masked to the prefix", got.Providers[0].RefusalCodes)
	}
	if got.Providers[0].RefusalCooldownSecs != 900 {
		t.Errorf("per-provider cooldown = %d, want 900", got.Providers[0].RefusalCooldownSecs)
	}
	// A provider that names no codes must stay that way on the wire, so
	// rolodex substitutes its built-in set rather than being frozen to
	// whatever the codes happen to be today.
	if len(got.Providers[1].RefusalCodes) != 0 {
		t.Errorf("second provider codes = %v, want empty", got.Providers[1].RefusalCodes)
	}
}

// TestDNSDnsblRefusalCodesRoundtrip is the DNSBL twin — the list that actually
// affects browsing, and the one an over-quota Spamhaus answer would take down.
func TestDNSDnsblRefusalCodesRoundtrip(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{{
		Zone:                "dbl.spamhaus.org",
		Enabled:             true,
		RefusalCodes:        []string{"127.255.255.0/24", "127.0.1.255"},
		RefusalCooldownSecs: 60,
	}}
	if err := c.SetDnsblConfig(ctx, true, providers, 120); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	got, err := c.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if got.RefusalCooldownSecs != 120 {
		t.Errorf("list-wide cooldown = %d, want 120", got.RefusalCooldownSecs)
	}
	if len(got.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %+v", got.Providers)
	}
	if !slices.Equal(got.Providers[0].RefusalCodes, []string{"127.255.255.0/24", "127.0.1.255"}) {
		t.Errorf("refusal codes = %v", got.Providers[0].RefusalCodes)
	}
	if got.Providers[0].RefusalCooldownSecs != 60 {
		t.Errorf("per-provider cooldown = %d, want 60", got.Providers[0].RefusalCooldownSecs)
	}
}

// TestDNSRblRefusalCodesNoneDisablesDetection proves the opt-out reaches
// rolodex intact, for a private blocklist whose real listings collide with one
// of the built-in codes.
func TestDNSRblRefusalCodesNoneDisablesDetection(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{
		{Zone: "rbl.example.com", Enabled: true, RefusalCodes: []string{"NONE"}},
	}
	if err := c.SetRblConfig(ctx, true, providers, 0); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	got, err := c.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if len(got.Providers) != 1 || !slices.Equal(got.Providers[0].RefusalCodes, []string{"none"}) {
		t.Fatalf("expected the opt-out preserved as [none], got %+v", got.Providers)
	}
}

// TestDNSRblRefusalCodesInvalid rejects a malformed code at the API boundary,
// where the operator sees the error, rather than storing it and having rolodex
// refuse the config later.
func TestDNSRblRefusalCodesInvalid(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		codes []string
	}{
		{"not an address", []string{"over-quota"}},
		{"prefix too long", []string{"127.0.0.1/33"}},
		{"ipv6", []string{"::1"}},
		{"none mixed with codes", []string{"none", "127.0.0.1"}},
		{"duplicate after masking", []string{"127.255.255.0/24", "127.255.255.9/24"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
				{Zone: "zen.spamhaus.org", Enabled: true, RefusalCodes: tc.codes},
			}, 0)
			if err == nil {
				t.Fatalf("expected an error for refusal codes %v", tc.codes)
			}
		})
	}
}

// TestDNSRblRotatedOutReported surfaces the providers rolodex has taken out of
// the lookup rotation after a refusal. Without it the operator's only signal
// that a blocklist stopped being consulted is that it stopped blocking things.
func TestDNSRblRotatedOutReported(t *testing.T) {
	t.Parallel()
	c, rc := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}
	rc.RblRotatedOut = []*upstream.RotatedProvider{
		{Zone: "zen.spamhaus.org", Code: "127.255.255.254", SecondsRemaining: 3212},
	}

	got, err := c.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if len(got.RotatedOut) != 1 {
		t.Fatalf("expected 1 rotated-out provider, got %+v", got.RotatedOut)
	}
	r := got.RotatedOut[0]
	if r.Zone != "zen.spamhaus.org" || r.Code != "127.255.255.254" || r.SecondsRemaining != 3212 {
		t.Errorf("unexpected rotated-out entry: %+v", r)
	}
}

// TestDNSDnsblRotatedOutReported is the DNSBL twin; the two lists have
// independent rotations, so a refusal on one must not appear on the other.
func TestDNSDnsblRotatedOutReported(t *testing.T) {
	t.Parallel()
	c, rc := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.SetDnsblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "dbl.spamhaus.org", Enabled: true},
	}, 0); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}
	rc.DnsblRotatedOut = []*upstream.RotatedProvider{
		{Zone: "dbl.spamhaus.org", Code: "127.255.255.255", SecondsRemaining: 42},
	}

	dnsbl, err := c.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if len(dnsbl.RotatedOut) != 1 || dnsbl.RotatedOut[0].Code != "127.255.255.255" {
		t.Fatalf("unexpected DNSBL rotated-out: %+v", dnsbl.RotatedOut)
	}

	rbl, err := c.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if len(rbl.RotatedOut) != 0 {
		t.Errorf("DNSBL refusal leaked into the RBL rotation: %+v", rbl.RotatedOut)
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
