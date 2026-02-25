package packages

import (
	"fmt"
	"math/rand/v2"
	"net"
)

// FindAvailablePort picks a random port in the range 10000-60000 that is not
// in the excluded set and is not currently in use. It tries up to 100 times
// before returning an error.
func FindAvailablePort(excluded map[uint16]bool) (_ uint16, err error) {
	const minPort = 10000
	const maxPort = 60000

	for range 100 {
		port := uint16(minPort + rand.IntN(maxPort-minPort+1))
		if excluded[port] {
			continue
		}

		ln, listenErr := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if listenErr != nil {
			continue
		}
		if err = ln.Close(); err != nil {
			return 0, err
		}
		return port, nil
	}

	return 0, fmt.Errorf("could not find available port after 100 attempts")
}

// GenerateHostname returns a hostname for the given package name. If the name
// is already unique, it returns the name as-is. Otherwise it appends a random
// 4-character hex suffix.
func GenerateHostname(pkgName string) string {
	suffix := fmt.Sprintf("%04x", rand.IntN(0x10000))
	return fmt.Sprintf("%s-%s", pkgName, suffix)
}
