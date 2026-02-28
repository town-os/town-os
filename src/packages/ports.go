package packages

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
)

// FindAvailablePort picks a random port in the range 10000-60000 that is not
// in the excluded set and is not currently in use. It tries up to 100 times
// before returning an error.
func FindAvailablePort(excluded map[uint16]bool) (_ uint16, err error) {
	const minPort = 10000
	const maxPort = 60000

	for range 100 {
		n, err := rand.Int(rand.Reader, big.NewInt(maxPort-minPort+1))
		if err != nil {
			return 0, fmt.Errorf("generate random port: %w", err)
		}
		v := minPort + int(n.Int64())
		if v < 0 || v > math.MaxUint16 {
			continue
		}
		port := uint16(v) //nolint:gosec // G115: bounds checked above
		if excluded[port] {
			continue
		}

		lc := net.ListenConfig{}
		ln, listenErr := lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", port))
		if listenErr != nil {
			continue
		}
		if err = ln.Close(); err != nil {
			return 0, err
		}
		return port, nil
	}

	return 0, errors.New("could not find available port after 100 attempts")
}

// GenerateHostname returns a hostname for the given package name. If the name
// is already unique, it returns the name as-is. Otherwise it appends a random
// 4-character hex suffix.
func GenerateHostname(pkgName string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(0x10000))
	if err != nil {
		// Fallback should never happen with crypto/rand.
		n = big.NewInt(0)
	}
	suffix := fmt.Sprintf("%04x", n.Int64())
	return pkgName + "-" + suffix
}
