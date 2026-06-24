package systemcontroller

import (
	"net"
	"strings"
)

// IsVirtualInterface reports whether an interface name belongs to a container
// runtime, virtual bridge, or VPN tunnel. Addresses on these interfaces (e.g.
// the podman bridge 10.88.0.1) are never the host's real LAN/WAN address and
// must be skipped when discovering the address to publish in DNS.
func IsVirtualInterface(name string) bool {
	for _, prefix := range []string{
		"podman", "veth", "cni", "docker", "br-", "virbr", "tailscale",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// firstIPv4 returns the first non-loopback IPv4 address in addrs, or "".
func firstIPv4(addrs []net.Addr) string {
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

// firstGlobalIPv6 returns the first globally routable IPv6 address in addrs, or
// "". Link-local (fe80::, IsGlobalUnicast is false) and ULA (fc00::/7, reported
// by IsPrivate) are excluded: only an address reachable off-link is worth
// publishing as a AAAA record. Unlike IPv4 (which is typically a private,
// NAT'd LAN address), a host's global IPv6 is usually directly reachable, so
// this doubles as the publicly resolvable AAAA target.
func firstGlobalIPv6(addrs []net.Addr) string {
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.To4() == nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
			return ip.String()
		}
	}
	return ""
}

// InternalInterfaceIPs walks the host's network interfaces (skipping loopback,
// down, and virtual interfaces) and returns the IPv4 and global IPv6 addresses
// of the first interface that bears a usable IPv4. Both addresses come from the
// SAME interface so a package's DNS A and AAAA records point at one NIC. This
// is the single source of truth for the systemcontroller's internal-address
// discovery, used by both the boot reconcile and the runtime poller. Either
// return value may be "" — an IPv4-only host yields ipv6 == "", and a host with
// no usable physical interface yields both "".
func InternalInterfaceIPs() (ipv4, ipv6 string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if IsVirtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		if v4 := firstIPv4(addrs); v4 != "" {
			return v4, firstGlobalIPv6(addrs)
		}
	}
	return "", ""
}
