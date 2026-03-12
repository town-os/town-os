package systemcontroller

import (
	"strings"
	"testing"
)

func TestValidateTLDValid(t *testing.T) {
	valid := []string{"home", "local", "lan", "my-net", "a", "abc123", "a-b-c"}
	for _, tld := range valid {
		if err := ValidateTLD(tld); err != nil {
			t.Errorf("ValidateTLD(%q) = %v, want nil", tld, err)
		}
	}
}

func TestValidateTLDInvalid(t *testing.T) {
	cases := []struct {
		tld  string
		want string
	}{
		{"", "must not be empty"},
		{"Home", "invalid"},
		{"UPPER", "invalid"},
		{"-bad", "invalid"},
		{"bad-", "invalid"},
		{"a.b", "invalid"},
		{strings.Repeat("a", 64), "at most 63"},
		{"hello world", "invalid"},
		{"test!", "invalid"},
		{"under_score", "invalid"},
	}
	for _, tc := range cases {
		err := ValidateTLD(tc.tld)
		if err == nil {
			t.Errorf("ValidateTLD(%q) = nil, want error containing %q", tc.tld, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateTLD(%q) = %v, want error containing %q", tc.tld, err, tc.want)
		}
	}
}

func TestValidateTLDMaxLength(t *testing.T) {
	// Exactly 63 characters should be valid.
	tld63 := strings.Repeat("a", 63)
	if err := ValidateTLD(tld63); err != nil {
		t.Errorf("ValidateTLD(63 chars) = %v, want nil", err)
	}

	// 64 characters should be invalid.
	tld64 := strings.Repeat("a", 64)
	if err := ValidateTLD(tld64); err == nil {
		t.Error("ValidateTLD(64 chars) = nil, want error")
	}
}
