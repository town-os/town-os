// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"encoding/hex"
	"errors"
	"unicode/utf16"

	"golang.org/x/crypto/md4" //nolint:staticcheck,gosec // SA1019,G501 -- MD4 is not a choice: NTLMv2 is defined over MD4(UTF16LE(password)) and nothing else verifies against a Windows client
)

// The SMB credential.
//
// SMB authenticates with NTLMv2, which is computed under
// NT_hash = MD4(UTF16LE(password)). Town OS stores account passwords as
// bcrypt, and **bcrypt cannot be converted into an NT hash** — they are
// different one-way functions over the same input, with no path between them
// in either direction. So an account that wants to mount an object-storage
// share enrols a second secret, and this is where the plaintext turns into the
// only form that is kept.
//
// This is the same reason Samba maintains its own password database, and it is
// not a shortcut anybody can remove: the alternative is SMB that authenticates
// nobody.

// ErrSMBPasswordTooShort is returned for a credential below the minimum.
var ErrSMBPasswordTooShort = errors.New("smb password must be at least 8 characters")

// MinSMBPasswordLength matches the account password minimum. An NT hash has no
// work factor and no salt, so a short one is materially easier to attack
// offline than a short bcrypt password — the floor is if anything more
// important here.
const MinSMBPasswordLength = 8

// NTHash derives the NTLM hash of a password: MD4 of its UTF-16LE encoding.
//
// Returned hex-encoded, lowercase, 32 characters — the form gfehd's config
// takes and the form stored on the account.
//
// The UTF-16LE encoding is part of the definition, not an implementation
// detail: hashing the UTF-8 bytes produces a value that is stable, plausible,
// and rejected by every Windows client.
func NTHash(password string) (string, error) {
	if len(password) < MinSMBPasswordLength {
		return "", ErrSMBPasswordTooShort
	}

	// Surrogate pairs are encoded as two code units, which is what a Windows
	// client does with an astral character too.
	units := utf16.Encode([]rune(password))
	encoded := make([]byte, 0, len(units)*2)
	for _, u := range units {
		//nolint:gosec // G115 -- deliberate: these are the low and high bytes of a UTF-16 code unit, which is what little-endian encoding means
		encoded = append(encoded, byte(u&0xff), byte(u>>8))
	}

	//nolint:gosec // G401 -- see the package comment: MD4 is what NTLMv2 is defined over
	h := md4.New()
	if _, err := h.Write(encoded); err != nil {
		// md4.Write cannot fail, but the error is checked rather than
		// discarded because every error in this tree is.
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
