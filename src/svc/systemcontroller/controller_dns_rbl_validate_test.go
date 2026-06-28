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
