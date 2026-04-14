// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

// The server enforces a 7-bit printable ASCII band (0x21..0x7E) on every
// password submitted through /account/create and /account/update, and
// packages.GenerateSecret() draws from the same alphabet so auto-generated
// package secrets can round-trip as account passwords without tripping
// the validator. These tests drive both halves through the real HTTP
// stack: the validator via a running systemcontroller, and the generator
// by feeding its output to that validator.

func TestPasswordValidationRejectsNonASCII(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		password string
	}{
		{"latin1 umlaut", "pässword1"},
		{"raw high byte", "password\xe9"},
		{"emoji", "password\U0001F600"},
		{"embedded space", "pass word1"},
		{"leading space", " password1"},
		{"trailing space", "password1 "},
		{"tab", "pa\tssword1"},
		{"null byte", "pa\x00ssword"},
		{"DEL byte", "password\x7f"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _ := initBootstrapTest(t)

			_, err := c.CreateAccount(context.TODO(), "admin", tc.password, "a@test.com", "555-0000", "Admin", true)
			if err == nil {
				t.Fatalf("CreateAccount(%q) = nil, want rejection", tc.password)
			}
			if !strings.Contains(err.Error(), "printable ASCII") {
				t.Fatalf("CreateAccount(%q) error = %v, want substring %q", tc.password, err, "printable ASCII")
			}
		})
	}
}

func TestPasswordValidationAcceptsPrintableASCII(t *testing.T) {
	t.Parallel()

	// Full visible band plus a few letters and digits to hit the min length.
	const pw = `!"#$%&'()*+,-./0123456789abcXYZ`
	c, _ := initBootstrapTest(t)

	if _, err := c.CreateAccount(context.TODO(), "admin", pw, "a@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("CreateAccount with printable ASCII password: %v", err)
	}
	if _, err := c.Authenticate(context.TODO(), "admin", pw); err != nil {
		t.Fatalf("Authenticate with printable ASCII password: %v", err)
	}
}

// Closes the loop: a secret emitted by packages.GenerateSecret must be
// accepted as a password by the same server that enforces the ASCII band.
// If the generator and the validator ever drift apart, auto-generated
// package secrets will silently fail to round-trip at the account layer
// and this test will catch it.
func TestGeneratedSecretIsValidPassword(t *testing.T) {
	t.Parallel()
	c, _ := initBootstrapTest(t)

	secret, err := packages.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("GenerateSecret returned %d bytes, want 32: %q", len(secret), secret)
	}

	if _, err := c.CreateAccount(context.TODO(), "admin", secret, "a@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("CreateAccount with generated secret %q: %v", secret, err)
	}
	if _, err := c.Authenticate(context.TODO(), "admin", secret); err != nil {
		t.Fatalf("Authenticate with generated secret %q: %v", secret, err)
	}
}
