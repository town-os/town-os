// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	t.Run("alphanumeric output", func(t *testing.T) {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Length is measured in bytes, not runes, to guarantee the output
		// is exactly 32 bytes on the wire — any multi-byte rune would
		// violate the alphanumeric invariant being tested.
		if len(s) != 32 {
			t.Fatalf("expected 32-byte string, got %d bytes: %q", len(s), s)
		}
		for i := range len(s) {
			b := s[i]
			isUpper := b >= 'A' && b <= 'Z'
			isLower := b >= 'a' && b <= 'z'
			isDigit := b >= '0' && b <= '9'
			if !isUpper && !isLower && !isDigit {
				t.Fatalf("byte at index %d is not alphanumeric: 0x%02X (%q)", i, b, string(b))
			}
		}
	})

	t.Run("never emits transport-unsafe metacharacters", func(t *testing.T) {
		// Regression: a prior generator emitted the full printable 7-bit
		// ASCII range, which broke installed jitsi packages at runtime —
		// the space character split systemd ExecStart tokens (truncating
		// JVB_AUTH_PASSWORD) and backslash triggered HOCON JSON-escape
		// parsing in jvb.conf. The alphanumeric alphabet eliminates every
		// metacharacter across every transport Town OS touches. Run the
		// generator repeatedly so one pass exercises a broad slice of the
		// output distribution.
		const forbidden = " \\{}$\"'`#:;=<>|&()[],.*?!~^@/+%"
		for range 200 {
			s, err := GenerateSecret()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for i := range len(s) {
				b := s[i]
				for j := range len(forbidden) {
					if b == forbidden[j] {
						t.Fatalf("secret %q contains forbidden metacharacter %q at index %d", s, string(b), i)
					}
				}
			}
		}
	})

	t.Run("non-empty result", func(t *testing.T) {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s == "" {
			t.Fatal("expected non-empty secret")
		}
	})

	t.Run("generates different values", func(t *testing.T) {
		a, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		b, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a == b {
			t.Fatalf("expected different secrets, got identical: %q", a)
		}
	})
}
