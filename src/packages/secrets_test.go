// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	t.Run("valid latin1 output", func(t *testing.T) {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		runes := []rune(s)
		if len(runes) != 32 {
			t.Fatalf("expected 32-character string, got %d chars: %q", len(runes), s)
		}
		for i, r := range runes {
			if (r < 0x20 || r > 0x7E) && (r < 0xA0 || r > 0xFF) {
				t.Fatalf("character at index %d is not printable Latin-1: %U", i, r)
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
