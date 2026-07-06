package wireguard

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
)

// DefaultListenPortBase is the first UDP port assigned to network interfaces.
// Each network offsets from this base by its index so interfaces never collide.
const DefaultListenPortBase = 51820

// SubnetForNetwork derives a deterministic /24 overlay subnet for a network
// from the host's systemd machine-id and the network name. Keying on the
// machine-id ensures two Town OS boxes that both serve peers pick distinct
// subnets, so a peer joining both never sees an address collision.
//
// The subnet is drawn from 10.64.0.0/10 (10.64.x.x .. 10.127.x.x) to bias away
// from the 10.0/10.1 ranges commonly handed out by consumer routers.
func SubnetForNetwork(machineID, networkName string) (netip.Prefix, error) {
	if machineID == "" {
		return netip.Prefix{}, errors.New("machine id is required for subnet derivation")
	}
	h := sha256.Sum256([]byte(machineID + "|" + networkName))
	second := 64 + (h[0] % 64) // 64..127
	third := h[1]
	addr := netip.AddrFrom4([4]byte{10, second, third, 0})
	return netip.PrefixFrom(addr, 24), nil
}

// LocalAddr returns the box's own overlay address within the subnet, which is
// always the .1 host. Callers combine it with the prefix length to form the
// interface Address (see AddressCIDR).
func LocalAddr(subnet netip.Prefix) netip.Addr {
	a := subnet.Addr().As4()
	a[3] = 1
	return netip.AddrFrom4(a)
}

// AddressCIDR returns the interface address for the local box (".1" of the
// subnet) in CIDR form, e.g. "10.90.12.1/24".
func AddressCIDR(subnet netip.Prefix) string {
	return fmt.Sprintf("%s/%d", LocalAddr(subnet), subnet.Bits())
}

// AllocatePeerAddr picks a free host address in the subnet for a new peer.
// When seed is non-empty (e.g. the peer's machine-id or public key) the
// preferred host octet is derived from it so a given device receives a stable
// overlay address across re-adds; on collision it probes forward. Host octets
// .0, .1 (the box) and .255 are reserved. used holds the string forms of
// already-allocated addresses.
func AllocatePeerAddr(subnet netip.Prefix, used map[string]bool, seed string) (netip.Addr, error) {
	a := subnet.Addr().As4()

	start := 2
	if seed != "" {
		h := sha256.Sum256([]byte(seed))
		start = 2 + int(h[0])%253 // 2..254
	}

	const usableHosts = 253 // 2..254 inclusive
	for i := range usableHosts {
		host := 2 + (start-2+i)%usableHosts
		a[3] = byte(host) //nolint:gosec // G115: host is bounded to 2..254 by the modulo above
		cand := netip.AddrFrom4(a)
		if !used[cand.String()] {
			return cand, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("overlay subnet %s is exhausted", subnet)
}

// ListenPortForIndex returns the UDP listen port for the network at the given
// zero-based index.
func ListenPortForIndex(index int) int {
	return DefaultListenPortBase + index
}

// ListenPortForName derives a stable candidate UDP listen port for a network
// from its name. Keying on the name (rather than a creation-order index) keeps
// the port stable and avoids the collision where removing then re-creating a
// network reuses a port still held by another network. Callers probe forward
// from this candidate to resolve the rare hash collision between two names.
func ListenPortForName(name string) int {
	h := sha256.Sum256([]byte("port|" + name))
	offset := (int(h[0])<<8 | int(h[1])) % 4096
	return DefaultListenPortBase + offset
}
