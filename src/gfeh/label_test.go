// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"strings"
	"testing"
)

func TestNormalizeLabel(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"s3.gfeh", "s3.gfeh"},
		{"  s3.gfeh  ", "s3.gfeh"},
		{"s3.gfeh.", "s3.gfeh"},
		{"S3.GFEH", "s3.gfeh"},
		{"", ""},
		{"   ", ""},
	} {
		if got := NormalizeLabel(tc.in); got != tc.want {
			t.Errorf("NormalizeLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The labels gfehctl actually renders must all pass, or object storage silently
// publishes nothing.
func TestValidateLabelAcceptsEveryViewLabel(t *testing.T) {
	for _, view := range append([]string{ViewSMB}, HTTPViews...) {
		label := view + "." + VolumePrefix
		if err := ValidateLabel(label); err != nil {
			t.Errorf("ValidateLabel(%q) = %v, want nil", label, err)
		}
	}
	if err := ValidateLabel(IndexLabel); err != nil {
		t.Errorf("ValidateLabel(%q) = %v, want nil", IndexLabel, err)
	}
}

// Each of these is a shape that reaches a Caddyfile, a zone or a path, and each
// one is the reason the validator is strict rather than a slash check.
func TestValidateLabelRejects(t *testing.T) {
	for name, label := range map[string]string{
		"empty":               "",
		"path traversal":      "../../etc/passwd",
		"single dot segment":  "s3/./gfeh",
		"slash":               "s3/gfeh",
		"caddyfile injection": "s3.gfeh {\n}\nhttps://evil.home {",
		"newline":             "s3.gfeh\nevil",
		"space":               "s3 gfeh",
		"wildcard":            "*.gfeh",
		"leading dot":         ".gfeh",
		"doubled dot":         "s3..gfeh",
		"underscore":          "s3_view.gfeh",
		"leading hyphen":      "-s3.gfeh",
		"trailing hyphen":     "s3-.gfeh",
		"null byte":           "s3.gfeh\x00",
		"non-ascii":           "s3.gfëh",
		"colon port":          "s3.gfeh:9000",
		"at sign":             "s3@gfeh",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLabel(label); err == nil {
				t.Errorf("ValidateLabel(%q) = nil, want an error", label)
			}
		})
	}
}

func TestValidateLabelLengthLimits(t *testing.T) {
	long := strings.Repeat("a", LabelMaxLen+1)
	if err := ValidateLabel(long + ".gfeh"); err == nil {
		t.Error("a component over the DNS label limit was accepted")
	}

	whole := strings.Repeat("abc.", (NameMaxLen/4)+1)
	if err := ValidateLabel(whole + "gfeh"); err == nil {
		t.Error("a name over the DNS name limit was accepted")
	}
}
