// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	t.Run("valid 7-bit ASCII output", func(t *testing.T) {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Length must be measured in bytes, not runes, to guarantee the
		// output is exactly 32 bytes on the wire — any multi-byte rune
		// would violate the 7-bit invariant we are testing for.
		if len(s) != 32 {
			t.Fatalf("expected 32-byte string, got %d bytes: %q", len(s), s)
		}
		for i := range len(s) {
			b := s[i]
			if b < 0x21 || b > 0x7E {
				t.Fatalf("byte at index %d is not printable 7-bit ASCII: 0x%02X", i, b)
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
