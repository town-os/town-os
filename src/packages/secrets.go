package packages

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// secretAlphabet is the 62-character alphanumeric set — A-Z, a-z, 0-9.
// Every punctuation mark is excluded: package secrets get injected into
// systemd ExecStart lines, podman -e KEY=VALUE args, HOCON config files
// (jitsi jvb.conf, jicofo.conf), YAML, JSON, shell eval, properties files,
// SQL string literals, and URLs. Every punctuation character is a
// metacharacter in at least one of those transports — space splits
// ExecStart tokens, backslash triggers HOCON JSON-escape parsing, braces
// delimit HOCON objects and bash expansion, dollar triggers substitution,
// quotes terminate strings, colon and equals are key/value separators,
// hash opens comments, and so on. Restricting the alphabet is the only
// reliable way to produce a secret that survives every transport Town OS
// exercises without per-transport escaping logic at every emission site.
var secretAlphabet = func() []byte {
	chars := make([]byte, 0, 62)
	for c := byte('A'); c <= 'Z'; c++ {
		chars = append(chars, c)
	}
	for c := byte('a'); c <= 'z'; c++ {
		chars = append(chars, c)
	}
	for c := byte('0'); c <= '9'; c++ {
		chars = append(chars, c)
	}
	return chars
}()

// GenerateSecret returns a cryptographically secure random 32-character
// string drawn from the alphanumeric alphabet (A-Z, a-z, 0-9). The result
// is safe for use as package-level secrets (auto-generated passwords,
// encryption key salts, signing keys) on every transport Town OS exercises:
// systemd unit lines, HOCON, JSON, YAML, shell, SQL, and URLs. 32 characters
// over a 62-character alphabet gives ~190 bits of entropy, well above any
// realistic brute-force budget.
func GenerateSecret() (string, error) {
	n := big.NewInt(int64(len(secretAlphabet)))
	result := make([]byte, 32)
	for i := range result {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("generate secret: %w", err)
		}
		result[i] = secretAlphabet[idx.Int64()]
	}
	return string(result), nil
}
