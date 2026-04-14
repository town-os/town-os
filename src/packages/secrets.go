package packages

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// asciiPrintable is the 94-character set of visible 7-bit ASCII — letters,
// digits, and every punctuation mark in the range 0x21..0x7E. Space (0x20),
// DEL (0x7F), every control character, and the entire 8-bit plane are
// excluded so the generated value survives HTTP Basic auth, JSON, URL
// encoding, shell eval, and MySQL latin1 columns without any codec
// round-trip hazards. Matches the character set enforced by
// src/account/account.go:validatePassword.
var asciiPrintable = func() []byte {
	chars := make([]byte, 0, 0x7E-0x21+1)
	for c := byte(0x21); c <= 0x7E; c++ {
		chars = append(chars, c)
	}
	return chars
}()

// GenerateSecret returns a cryptographically secure random 32-character
// string drawn from the printable 7-bit ASCII alphabet (94 characters).
// The result is safe for use as package-level secrets (auto-generated
// passwords, encryption key salts, signing keys) on every transport
// Town OS exercises. 32 characters over a 94-character alphabet gives
// ~209 bits of entropy, well above any realistic brute-force budget.
func GenerateSecret() (string, error) {
	n := big.NewInt(int64(len(asciiPrintable)))
	result := make([]byte, 32)
	for i := range result {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("generate secret: %w", err)
		}
		result[i] = asciiPrintable[idx.Int64()]
	}
	return string(result), nil
}
