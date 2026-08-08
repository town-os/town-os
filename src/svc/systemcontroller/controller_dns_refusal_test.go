package systemcontroller

import (
	"slices"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// TestValidateRefusalCodesEmptyMeansBuiltIn pins the case an existing
// configuration is in: no codes named, which must be passed through as "no
// codes named" so rolodex substitutes its built-in set. Rewriting it into an
// explicit list here would freeze today's codes into every stored config, and a
// provider that adds a new error code later would start being read as a listing.
func TestValidateRefusalCodesEmptyMeansBuiltIn(t *testing.T) {
	for _, in := range [][]string{nil, {}} {
		got, err := ValidateRefusalCodes(in)
		if err != nil {
			t.Fatalf("ValidateRefusalCodes(%v) = %v, want nil", in, err)
		}
		if len(got) != 0 {
			t.Errorf("ValidateRefusalCodes(%v) = %v, want empty", in, got)
		}
	}
}

// TestValidateRefusalCodesNoneAlone accepts the opt-out spelling, which is the
// only way to say "this provider has no refusal codes" — an empty list already
// means "use the built-in set".
func TestValidateRefusalCodesNoneAlone(t *testing.T) {
	for _, in := range []string{"none", "NONE", "  None  "} {
		got, err := ValidateRefusalCodes([]string{in})
		if err != nil {
			t.Fatalf("ValidateRefusalCodes([%q]) = %v, want nil", in, err)
		}
		if !slices.Equal(got, []string{RefusalCodesNone}) {
			t.Errorf("ValidateRefusalCodes([%q]) = %v, want [%q]", in, got, RefusalCodesNone)
		}
	}
}

// TestValidateRefusalCodesNoneWithOthers rejects a list that both disables
// detection and names codes to detect: there is no reading to pick, and
// silently preferring either one would surprise the operator in the direction
// they did not choose.
func TestValidateRefusalCodesNoneWithOthers(t *testing.T) {
	cases := [][]string{
		{"none", "127.0.0.1"},
		{"127.0.0.1", "none"},
		{"127.0.0.1", "NONE", "127.0.0.255"},
	}
	for _, in := range cases {
		_, err := ValidateRefusalCodes(in)
		if err == nil {
			t.Fatalf("ValidateRefusalCodes(%v) = nil, want error", in)
		}
		if !strings.Contains(err.Error(), "must be the only entry") {
			t.Errorf("ValidateRefusalCodes(%v) error = %v, want 'must be the only entry'", in, err)
		}
	}
}

// TestValidateRefusalCodesNormalizesToPrefix proves a code is masked to its
// prefix on the way in, matching how rolodex stores and reports one back. A
// GET that appeared to rewrite what the operator typed reads as the box having
// ignored them.
func TestValidateRefusalCodesNormalizesToPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"127.0.0.1", "127.0.0.1"},
		{"  127.0.0.255  ", "127.0.0.255"},
		{"127.255.255.0/24", "127.255.255.0/24"},
		{"127.255.255.254/24", "127.255.255.0/24"}, // base masked to the prefix
		{"127.0.1.255/32", "127.0.1.255"},          // /32 renders bare
		{"127.0.0.0/8", "127.0.0.0/8"},
		{"127.1.2.3/0", "0.0.0.0/0"},
	}
	for _, tc := range cases {
		got, err := ValidateRefusalCodes([]string{tc.in})
		if err != nil {
			t.Fatalf("ValidateRefusalCodes([%q]) = %v, want nil", tc.in, err)
		}
		if !slices.Equal(got, []string{tc.want}) {
			t.Errorf("ValidateRefusalCodes([%q]) = %v, want [%q]", tc.in, got, tc.want)
		}
	}
}

// TestValidateRefusalCodesInvalid rejects anything rolodex would reject, so a
// bad code is reported at the API boundary rather than accepted here and then
// refused by the DNS server, where the operator would not see it.
func TestValidateRefusalCodesInvalid(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{"", "must not be empty"},
		{"   ", "must not be empty"},
		{"not-an-ip", "IPv4 address"},
		{"127.0.0.1/33", "between 0 and 32"},
		{"127.0.0.1/-1", "between 0 and 32"},
		{"127.0.0.1/abc", "prefix must be a number"},
		{"127.0.0.256", "IPv4 address"},
		{"::1", "must be IPv4"},
		{"::ffff:127.0.0.1", "must be IPv4"},
		{"2001:db8::/32", "must be IPv4"},
	}
	for _, tc := range cases {
		_, err := ValidateRefusalCodes([]string{tc.code})
		if err == nil {
			t.Fatalf("ValidateRefusalCodes([%q]) = nil, want error", tc.code)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateRefusalCodes([%q]) error = %v, want it to mention %q", tc.code, err, tc.want)
		}
	}
}

// TestValidateRefusalCodesRejectsDuplicates catches two entries that mean the
// same thing after masking, which is a mistake worth telling the operator about
// rather than silently collapsing.
func TestValidateRefusalCodesRejectsDuplicates(t *testing.T) {
	cases := [][]string{
		{"127.0.0.1", "127.0.0.1"},
		{"127.255.255.0/24", "127.255.255.254/24"},
	}
	for _, in := range cases {
		_, err := ValidateRefusalCodes(in)
		if err == nil {
			t.Fatalf("ValidateRefusalCodes(%v) = nil, want error", in)
		}
		if !strings.Contains(err.Error(), "duplicate refusal code") {
			t.Errorf("ValidateRefusalCodes(%v) error = %v, want 'duplicate refusal code'", in, err)
		}
	}
}

// TestValidateRefusalCodesAcceptsSpamhausErrorRange is the case that motivates
// the whole field: Spamhaus answers a household box that has exceeded its
// free-use limit with a code in this range, and reading it as a listing would
// NXDOMAIN every name checked against the provider.
func TestValidateRefusalCodesAcceptsSpamhausErrorRange(t *testing.T) {
	got, err := ValidateRefusalCodes([]string{"127.255.255.0/24", "127.0.1.255", "127.0.2.255"})
	if err != nil {
		t.Fatalf("ValidateRefusalCodes: %v", err)
	}
	want := []string{"127.255.255.0/24", "127.0.1.255", "127.0.2.255"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestValidateProviderDTOsCarriesRefusalSettings proves the per-provider
// settings survive validation rather than being dropped on the floor — the
// zone-only validator they were bolted onto happily returned a provider with
// its refusal handling silently reset.
func TestValidateProviderDTOsCarriesRefusalSettings(t *testing.T) {
	cleaned, err := validateProviderDTOs([]RblProviderDTO{
		{
			Zone:                "  ZEN.Spamhaus.ORG ",
			Enabled:             true,
			RefusalCodes:        []string{"127.255.255.9/24"},
			RefusalCooldownSecs: 900,
		},
		{Zone: "bl.spamcop.net", Enabled: false},
	})
	if err != nil {
		t.Fatalf("validateProviderDTOs: %v", err)
	}
	if len(cleaned) != 2 {
		t.Fatalf("got %d providers, want 2", len(cleaned))
	}
	if cleaned[0].Zone != "zen.spamhaus.org" {
		t.Errorf("zone = %q, want normalized to lowercase", cleaned[0].Zone)
	}
	if !slices.Equal(cleaned[0].RefusalCodes, []string{"127.255.255.0/24"}) {
		t.Errorf("refusal codes = %v, want [127.255.255.0/24]", cleaned[0].RefusalCodes)
	}
	if cleaned[0].RefusalCooldownSecs != 900 {
		t.Errorf("cooldown = %d, want 900", cleaned[0].RefusalCooldownSecs)
	}
	if len(cleaned[1].RefusalCodes) != 0 {
		t.Errorf("second provider codes = %v, want empty (built-in set)", cleaned[1].RefusalCodes)
	}
}

// TestValidateProviderDTOsNamesTheZoneInRefusalErrors keeps a bad code
// attributable: a list of a dozen providers rejected with only "invalid refusal
// code" leaves the operator to guess which row they broke.
func TestValidateProviderDTOsNamesTheZoneInRefusalErrors(t *testing.T) {
	_, err := validateProviderDTOs([]RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "bl.spamcop.net", Enabled: true, RefusalCodes: []string{"nope"}},
	})
	if err == nil {
		t.Fatal("expected an error for the invalid refusal code")
	}
	if !strings.Contains(err.Error(), "bl.spamcop.net") {
		t.Errorf("error = %v, want it to name the offending zone", err)
	}
}

// TestRblProvidersToDTOCarriesResolvedRefusalCodes proves the codes rolodex
// reports as actually in effect reach the client. Dropping them would leave an
// operator reading an empty list on a provider that is in fact matching the
// built-in set.
func TestRblProvidersToDTOCarriesResolvedRefusalCodes(t *testing.T) {
	got := rblProvidersToDTO([]*upstream.RblConfig{
		{
			Zone:                "zen.spamhaus.org",
			Enabled:             true,
			RefusalCodes:        []string{"127.255.255.0/24", "127.0.0.1"},
			RefusalCooldownSecs: 1800,
		},
		nil, // rolodex is allowed to hand back a nil entry; it must be skipped
	})
	if len(got) != 1 {
		t.Fatalf("got %d providers, want 1", len(got))
	}
	if !slices.Equal(got[0].RefusalCodes, []string{"127.255.255.0/24", "127.0.0.1"}) {
		t.Errorf("refusal codes = %v", got[0].RefusalCodes)
	}
	if got[0].RefusalCooldownSecs != 1800 {
		t.Errorf("cooldown = %d, want 1800", got[0].RefusalCooldownSecs)
	}
}

// TestDnsblProvidersToDTOCarriesResolvedRefusalCodes is the DNSBL twin. The two
// conversions are separate functions over separate upstream types, so a fix to
// one does not fix the other.
func TestDnsblProvidersToDTOCarriesResolvedRefusalCodes(t *testing.T) {
	got := dnsblProvidersToDTO([]*upstream.DnsblConfig{
		{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCodes: []string{"127.0.1.255"}, RefusalCooldownSecs: 60},
		nil,
	})
	if len(got) != 1 {
		t.Fatalf("got %d providers, want 1", len(got))
	}
	if !slices.Equal(got[0].RefusalCodes, []string{"127.0.1.255"}) {
		t.Errorf("refusal codes = %v", got[0].RefusalCodes)
	}
	if got[0].RefusalCooldownSecs != 60 {
		t.Errorf("cooldown = %d, want 60", got[0].RefusalCooldownSecs)
	}
}

// TestRotatedOutToDTO converts the operator-visible half: which providers are
// currently not being asked, and why.
func TestRotatedOutToDTO(t *testing.T) {
	got := rotatedOutToDTO([]*upstream.RotatedProvider{
		{Zone: "zen.spamhaus.org", Code: "127.255.255.254", SecondsRemaining: 3212},
		nil,
	})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Zone != "zen.spamhaus.org" || got[0].Code != "127.255.255.254" || got[0].SecondsRemaining != 3212 {
		t.Errorf("unexpected entry: %+v", got[0])
	}
}

// TestRotatedOutToDTOEmpty keeps the nothing-wrong case an empty list rather
// than a nil that the omitempty tag would drop entirely — both render as "no
// providers rotated out", but an explicit empty is what a client can compare
// against without special-casing a missing field.
func TestRotatedOutToDTOEmpty(t *testing.T) {
	got := rotatedOutToDTO(nil)
	if got == nil {
		t.Fatal("rotatedOutToDTO(nil) = nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
