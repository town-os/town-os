package packages

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateSecret returns a cryptographically secure random 64-character hex
// string (256 bits of entropy). It is safe for use as passwords, encryption
// key salts, and other secret values.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
