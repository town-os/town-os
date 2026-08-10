package rolodex

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"strings"
)

// hostResolvConfPaths are the files consulted, in order, to discover the
// resolvers this box's own network handed it. The first file that yields at
// least one usable address wins.
//
// The systemd-resolved uplink file comes first and is the one that actually
// answers the question. On a Town OS box /etc/resolv.conf is a symlink to
// resolved's stub (127.0.0.53), so reading it alone yields a loopback address,
// which is filtered out and leaves nothing — the DHCP-provided servers live
// only in the uplink file. It is reachable from inside the systemcontroller
// container because the unit bind-mounts /run/systemd. /etc/resolv.conf is the
// fallback for a host not running systemd-resolved, where it holds the real
// servers directly.
var hostResolvConfPaths = []string{
	"/run/systemd/resolve/resolv.conf",
	"/etc/resolv.conf",
}

// HostResolvers returns the DNS servers this box's own network configuration
// points at, discovered from hostResolvConfPaths. See HostResolversFrom.
func HostResolvers() []string {
	return HostResolversFrom(hostResolvConfPaths...)
}

// HostResolversFrom returns the DNS servers named by the first of paths that
// yields a usable address, as "host:port" strings ready to be written into
// rolodex.yml as forwarders.
//
// These are the resolvers that keep working on a network that blocks external
// DNS: a hotel, a captive portal, or an ISP that drops outbound :53 to anywhere
// but its own servers will still answer queries sent to the resolver it handed
// out over DHCP. Rolodex's `auto` chain already falls back to its forwarders
// when the roots and the encrypted upstreams fail, but the forwarders Town OS
// writes by default are public resolvers (DefaultForwarders) — precisely the
// addresses such a network drops. Pointing that tier at the local resolver is
// what makes the fallback able to fall back to something.
//
// Loopback addresses are always discarded. On a resolved box the stub is
// 127.0.0.53 and rolodex itself binds DNSLoopback (127.0.0.2): forwarding to
// either is a query loop, not an upstream. An empty result means nothing usable
// was found and the caller should keep the forwarders it already had.
func HostResolversFrom(paths ...string) []string {
	for _, path := range paths {
		data, err := os.ReadFile(path) //nolint:gosec // G304 -- system paths, or a test fixture set through rolodex.Config
		if err != nil {
			continue
		}
		if addrs := parseResolvConf(data); len(addrs) > 0 {
			return addrs
		}
	}
	return nil
}

// parseResolvConf extracts usable forwarder addresses from resolv.conf-format
// bytes, in file order, de-duplicated, each with the default DNS port applied.
func parseResolvConf(data []byte) []string {
	var addrs []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(stripComment(scanner.Text()))
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip == nil || !usableForwarder(ip) {
			continue
		}
		addr := net.JoinHostPort(ip.String(), DefaultDNSPort)
		if seen[addr] {
			continue
		}
		seen[addr] = true
		addrs = append(addrs, addr)
	}

	return addrs
}

// stripComment removes a resolv.conf comment. Both '#' and ';' introduce one,
// and glibc honours them anywhere on the line, not just at its start.
func stripComment(line string) string {
	if i := strings.IndexAny(line, "#;"); i >= 0 {
		return line[:i]
	}
	return line
}

// usableForwarder reports whether ip is an address rolodex could actually
// forward a query to.
//
// Loopback is the case that matters: it is what a resolved stub (127.0.0.53)
// and rolodex's own listener (DNSLoopback) look like, and forwarding to either
// sends the query straight back to the resolver that asked it. The unspecified
// and multicast forms are not addresses of a resolver at all; a link-local
// address is dropped because it is only meaningful with the zone this process
// cannot know from a resolv.conf line.
func usableForwarder(ip net.IP) bool {
	return !ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast()
}
