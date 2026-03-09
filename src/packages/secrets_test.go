// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"encoding/hex"
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	t.Run("valid hex output", func(t *testing.T) {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(s) != 64 {
			t.Fatalf("expected 64-char hex string, got %d chars: %q", len(s), s)
		}
		if _, err := hex.DecodeString(s); err != nil {
			t.Fatalf("expected valid hex, got decode error: %v", err)
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
