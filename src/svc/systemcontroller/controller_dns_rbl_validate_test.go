package systemcontroller

import (
	"strings"
	"testing"
)

func TestValidateRblZoneValid(t *testing.T) {
	valid := []string{
		"zen.spamhaus.org",
		"dbl.spamhaus.org",
		"bl.spamcop.net",
		"b.barracudacentral.org",
		"dnsbl.sorbs.net",
		"sub.domain.example.com",
		"a.b", // minimal two-label
	}
	for _, z := range valid {
		if err := ValidateRblZone(z); err != nil {
			t.Errorf("ValidateRblZone(%q) = %v, want nil", z, err)
		}
	}
}

func TestValidateRblZoneInvalid(t *testing.T) {
	cases := []struct {
		zone string
		want string
	}{
		{"", "empty"},
		{"single", "fully-qualified"},
		{"-bad.example.com", "invalid"},
		{"bad-.example.com", "invalid"},
		{"under_score.example.com", "invalid"},
		{"has space.example.com", "invalid"},
		{strings.Repeat("a", 64) + ".com", "at most 63"},
	}
	for _, tc := range cases {
		err := ValidateRblZone(tc.zone)
		if err == nil {
			t.Errorf("ValidateRblZone(%q) = nil, want error containing %q", tc.zone, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateRblZone(%q) = %v, want error containing %q", tc.zone, err, tc.want)
		}
	}
}

func TestValidateLocalRblNameValid(t *testing.T) {
	valid := []string{
		"tracker.example.com",
		"ads.doubleclick.net",
		"192.0.2.10",          // IPv4
		"2001:db8::1",         // IPv6
		"a.b",                 // minimal domain
		"trailing.example.com.", // trailing dot tolerated
	}
	for _, n := range valid {
		if err := ValidateLocalRblName(n); err != nil {
			t.Errorf("ValidateLocalRblName(%q) = %v, want nil", n, err)
		}
	}
}

func TestValidateLocalRblNameInvalid(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "empty"},
		{"singlelabel", "domain name or IP"},
		{"bad domain.com", "invalid"},
		{"*.wildcard.com", "invalid"},
	}
	for _, tc := range cases {
		err := ValidateLocalRblName(tc.name)
		if err == nil {
			t.Errorf("ValidateLocalRblName(%q) = nil, want error containing %q", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateLocalRblName(%q) = %v, want error containing %q", tc.name, err, tc.want)
		}
	}
}

func TestValidateDnsblAllowlistNameValid(t *testing.T) {
	valid := []string{
		"example.com",
		"cdn.example.com",
		"a.b",                   // minimal domain
		"trailing.example.com.", // trailing dot tolerated
	}
	for _, n := range valid {
		if err := ValidateDnsblAllowlistName(n); err != nil {
			t.Errorf("ValidateDnsblAllowlistName(%q) = %v, want nil", n, err)
		}
	}
}

// An IP literal is the one case where the allowlist deliberately diverges from
// ValidateLocalRblName: the allowlist exempts a name from the *name-based*
// blocklist step and matches every name beneath it, so an address is not
// something it could ever match. Accepting one would store an entry that
// silently never fires.
func TestValidateDnsblAllowlistNameInvalid(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"singlelabel", "fully-qualified"},
		{"192.0.2.10", "not an IP address"},
		{"2001:db8::1", "not an IP address"},
		{"bad domain.com", "invalid"},
		{"*.wildcard.com", "invalid"},
		{"-bad.example.com", "invalid"},
		{strings.Repeat("a", 64) + ".com", "at most 63"},
		{strings.Repeat("a.", 200) + "com", "at most 253"},
	}
	for _, tc := range cases {
		err := ValidateDnsblAllowlistName(tc.name)
		if err == nil {
			t.Errorf("ValidateDnsblAllowlistName(%q) = nil, want error containing %q", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateDnsblAllowlistName(%q) = %v, want error containing %q", tc.name, err, tc.want)
		}
	}
}

// Rolodex hands allowlist names back fully-qualified; the handler presents the
// bare form so the Allow Lists table agrees with the Blocklists table beside it.
func TestTrimDNSRoot(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cdn.example.com.", "cdn.example.com"},
		{"cdn.example.com", "cdn.example.com"},
		{"example.com..", "example.com."}, // only one dot is removed
		{"", ""},
		{".", "."}, // the root is left alone rather than emptied
	}
	for _, tc := range cases {
		if got := trimDNSRoot(tc.in); got != tc.want {
			t.Errorf("trimDNSRoot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
