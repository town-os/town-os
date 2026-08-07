package packages

import "testing"

// CheckOAuthAddr is the dialer guard that stops a package-named URL from
// aiming the controller at the box's own network. TestCheckOAuthAddr covers the
// address classes Go's net package answers for directly -- loopback, private,
// link-local, multicast, unspecified -- plus the CGNAT range checkIP adds by
// hand.
//
// These are the ranges it still lets through. None of them is routable on the
// public internet, all of them are reachable inside a network the box sits on,
// and every one of them is a documented SSRF target:
//
//   - 0.0.0.0/8 beyond the exact unspecified address. checkIP tests
//     ip.IsUnspecified(), which is true only for 0.0.0.0 itself; Linux routes
//     the whole /8 to the local host, so http://0.0.0.1/ reaches loopback.
//   - 127.0.0.0/8 is covered by IsLoopback, but the IPv4-mapped and
//     NAT64 spellings of it are worth pinning explicitly.
//   - 192.0.0.0/24 (IETF protocol assignments) and 192.0.2.0/24, 198.51.100.0/24,
//     203.0.113.0/24 (documentation ranges) -- not routable.
//   - 198.18.0.0/15, the benchmarking range, which is used for real inside lab
//     networks.
//   - 240.0.0.0/4 (reserved) and 255.255.255.255 (limited broadcast).
//   - IPv6 site-local (fec0::/10), deprecated but still configured, and the
//     IPv4-mapped form of any of the above.
//
// A guard for this is an allowlist of "globally routable unicast", not a
// blocklist of the classes someone remembered. These tests assert the SECURE
// behaviour and fail against the current code.

func TestCheckOAuthAddrRejectsReservedRanges(t *testing.T) {
	t.Parallel()

	for name, addr := range map[string]string{
		"this-network 0.0.0.1":      "0.0.0.1:443",
		"this-network 0.1.2.3":      "0.1.2.3:443",
		"loopback via 127.1":        "127.0.0.2:443",
		"ipv4-mapped loopback":      "[::ffff:127.0.0.1]:443",
		"ipv4-mapped private":       "[::ffff:192.168.1.1]:443",
		"ipv4-mapped link-local":    "[::ffff:169.254.169.254]:443",
		"ietf protocol assignments": "192.0.0.1:443",
		"documentation tests-1":     "192.0.2.5:443",
		"documentation tests-2":     "198.51.100.5:443",
		"documentation tests-3":     "203.0.113.5:443",
		"benchmarking 198.18/15":    "198.18.0.5:443",
		"benchmarking 198.19/16":    "198.19.4.4:443",
		"reserved 240/4":            "240.0.0.1:443",
		"limited broadcast":         "255.255.255.255:443",
		"ipv6 site-local":           "[fec0::1]:443",
		"ipv6 discard-only":         "[100::1]:443",
		"nat64 well-known prefix":   "[64:ff9b::7f00:1]:443",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := CheckOAuthAddr("tcp", addr); err == nil {
				t.Errorf("CheckOAuthAddr(%q) was allowed; it is not a globally routable public address", addr)
			}
		})
	}
}

// The counterpart, so a fix cannot be "reject everything": real provider
// addresses must still dial. These are the shapes a device flow actually
// reaches -- Cloudflare, Google, and a public IPv6 endpoint.
func TestCheckOAuthAddrStillAllowsPublicAddresses(t *testing.T) {
	t.Parallel()

	for name, addr := range map[string]string{
		"cloudflare":     "104.18.32.1:443",
		"google dns":     "8.8.8.8:443",
		"public ipv6":    "[2606:4700::1]:443",
		"public ipv6 v2": "[2001:4860:4860::8888]:443",
		"high unicast":   "203.0.114.1:443",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := CheckOAuthAddr("tcp", addr); err != nil {
				t.Errorf("CheckOAuthAddr(%q) = %v, want allowed", addr, err)
			}
		})
	}
}
