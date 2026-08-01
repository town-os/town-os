// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"errors"
	"strings"
	"testing"
)

// TestNTHashMatchesTheSpecificationVector pins the derivation against MS-NLMP's
// own worked example rather than against this implementation.
//
// That matters more here than usual: the value is consumed by a different
// program, in a different language, over a wire protocol. A hash that is
// self-consistently wrong would pass every test on this side and fail every
// login on the other, with nothing in either log saying why.
func TestNTHashMatchesTheSpecificationVector(t *testing.T) {
	// MS-NLMP 4.2.1 common values: Password = "Password". 4.2.2.1.1 gives its
	// NTOWFv1 -- which IS the NT hash -- as this value.
	const password = "Password"
	const want = "a4f49c406510bdcab6824ee7c30fd852"

	got, err := NTHash(password)
	if err != nil {
		t.Fatalf("NTHash: %v", err)
	}
	if got != want {
		t.Errorf("NTHash(%q) = %s, want %s", password, got, want)
	}
}

// TestNTHashIsUTF16LE. Hashing the UTF-8 bytes instead produces a value that is
// stable, plausible, and rejected by every Windows client — the encoding is
// part of the definition, not an implementation detail.
func TestNTHashIsUTF16LE(t *testing.T) {
	// A password whose UTF-8 and UTF-16LE encodings differ beyond the padding:
	// if the implementation hashed UTF-8, this and the ASCII case could not
	// both match a real client.
	got, err := NTHash("pässwörd")
	if err != nil {
		t.Fatalf("NTHash: %v", err)
	}
	if len(got) != 32 {
		t.Errorf("hash is %d characters, want 32", len(got))
	}

	// Distinct passwords must not collide, including ones differing only in
	// non-ASCII characters.
	other, err := NTHash("passwörd")
	if err != nil {
		t.Fatalf("NTHash: %v", err)
	}
	if got == other {
		t.Error("two different passwords hashed the same")
	}
}

// TestNTHashHandlesAnAstralCharacter. utf16.Encode emits a surrogate pair,
// which is exactly what a Windows client does with one.
func TestNTHashHandlesAnAstralCharacter(t *testing.T) {
	got, err := NTHash("pass\U0001F600word")
	if err != nil {
		t.Fatalf("NTHash: %v", err)
	}
	if len(got) != 32 {
		t.Errorf("hash is %d characters, want 32", len(got))
	}
}

// TestNTHashEnforcesAMinimum. An NT hash is unsalted MD4 with no work factor,
// so a short one is materially easier to attack offline than a short bcrypt
// password — the floor matters more here, not less.
func TestNTHashEnforcesAMinimum(t *testing.T) {
	if _, err := NTHash("short"); !errors.Is(err, ErrSMBPasswordTooShort) {
		t.Errorf("err = %v, want ErrSMBPasswordTooShort", err)
	}
	if _, err := NTHash(""); !errors.Is(err, ErrSMBPasswordTooShort) {
		t.Errorf("empty password: err = %v, want ErrSMBPasswordTooShort", err)
	}
	if _, err := NTHash("12345678"); err != nil {
		t.Errorf("a password at the minimum was refused: %v", err)
	}
}

// TestNTHashIsLowercaseHex, because that is the form gfehd's config takes and
// the form its validator accepts.
func TestNTHashIsLowercaseHex(t *testing.T) {
	got, err := NTHash("Password")
	if err != nil {
		t.Fatalf("NTHash: %v", err)
	}
	if got != strings.ToLower(got) {
		t.Errorf("hash %q is not lowercase", got)
	}
	for _, r := range got {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("hash %q contains a non-hex character %q", got, r)
		}
	}
}

// TestSMBCredentialLifecycle: enrol, then withdraw.
//
// The three states have to stay distinguishable — nil leaves the credential
// alone, a password sets one, and the empty string revokes it. Collapsing the
// last two would make "no change" and "revoke" the same request.
func TestSMBCredentialLifecycle(t *testing.T) {
	mgr := InitMockManager()
	if _, err := mgr.Create("alice", "hunter2hunter2", "a@example.com", "5551234", "Alice", false); err != nil {
		t.Fatalf("create: %v", err)
	}

	acct, err := mgr.Get("alice")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if acct.SMBEnrolled || acct.SMBNTHash != "" {
		t.Error("a new account started with an SMB credential")
	}

	// A nil pointer leaves it alone.
	if _, err := mgr.Update("alice", UpdateFields{}); err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	acct, _ = mgr.Get("alice") //nolint:errcheck // asserted immediately below
	if acct.SMBEnrolled {
		t.Error("a no-op update enrolled a credential")
	}

	// A password enrols one.
	pw := "smbpassword"
	updated, err := mgr.Update("alice", UpdateFields{SMBPassword: &pw})
	if err != nil {
		t.Fatalf("enrol: %v", err)
	}
	if !updated.SMBEnrolled {
		t.Error("enrolling did not set SMBEnrolled")
	}
	want, err := NTHash(pw)
	if err != nil {
		t.Fatalf("NTHash: %v", err)
	}
	if updated.SMBNTHash != want {
		t.Errorf("stored hash = %q, want %q", updated.SMBNTHash, want)
	}

	// The empty string withdraws it.
	empty := ""
	withdrawn, err := mgr.Update("alice", UpdateFields{SMBPassword: &empty})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if withdrawn.SMBEnrolled || withdrawn.SMBNTHash != "" {
		t.Error("withdrawing left the credential in place")
	}
}

// TestSMBCredentialRejectsAShortPassword through the update path, not just the
// hash function.
func TestSMBCredentialRejectsAShortPassword(t *testing.T) {
	mgr := InitMockManager()
	if _, err := mgr.Create("alice", "hunter2hunter2", "a@example.com", "5551234", "Alice", false); err != nil {
		t.Fatalf("create: %v", err)
	}

	short := "abc"
	if _, err := mgr.Update("alice", UpdateFields{SMBPassword: &short}); !errors.Is(err, ErrSMBPasswordTooShort) {
		t.Errorf("err = %v, want ErrSMBPasswordTooShort", err)
	}
}
