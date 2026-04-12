package packages

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// latin1Printable contains all printable Latin-1 characters (0x20-0x7E, 0xA0-0xFF).
var latin1Printable = func() []rune {
	chars := make([]rune, 0, (0x7E-0x20+1)+(0xFF-0xA0+1))
	for c := rune(0x20); c <= 0x7E; c++ {
		chars = append(chars, c)
	}
	for c := rune(0xA0); c <= 0xFF; c++ {
		chars = append(chars, c)
	}
	return chars
}()

// GenerateSecret returns a cryptographically secure random 32-character string
// drawn from the printable Latin-1 character set (191 characters). It is safe
// for use as passwords, encryption key salts, and other secret values.
func GenerateSecret() (string, error) {
	n := big.NewInt(int64(len(latin1Printable)))
	result := make([]rune, 32)
	for i := range result {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("generate secret: %w", err)
		}
		result[i] = latin1Printable[idx.Int64()]
	}
	return string(result), nil
}
