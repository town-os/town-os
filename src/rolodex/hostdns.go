package rolodex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
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

// hostRouteTablePath is the kernel's IPv4 routing table, read to find the
// default gateway.
//
// The gateway is a forwarder candidate that resolv.conf cannot supply on a Town
// OS box, and on the networks this feature exists for it is usually the ONLY
// candidate. Three separate decisions conspire to empty every resolv.conf of a
// usable address: ttyforce writes `[DHCPv4] UseDNS=no` (and the v6 equivalent)
// on its networkd units so the DHCP-offered servers never become a per-link
// resolver that would outrank rolodex; bootstrap-dns.sh in ../install points
// systemd-resolved at DNSLoopback whenever rolodex is up, so the uplink file
// reads `nameserver 127.0.0.2`; and /etc/resolv.conf is resolved's own
// 127.0.0.53 stub. usableForwarder correctly discards all three as query loops,
// which left discovery with nothing to find on exactly the boxes it is for.
//
// The gateway survives all of that because it comes from the DHCP lease's
// router option rather than its DNS option, so suppressing DHCP DNS does not
// suppress it. In NAT and home-router setups it runs a forwarder — libvirt's
// dnsmasq on the QEMU box, the router on a home LAN, the captive appliance on
// hotel WiFi — and on a network that filters outbound :53 it is frequently the
// only address that answers at all. ttyforce reached the same conclusion for
// its own initrd DNS check (0.4.7) and folds the gateway into its candidate
// list for exactly this reason.
//
// Read from /proc rather than shelling out to `ip route`: the systemcontroller
// runs --net host, so this file is the host's own routing table, and parsing it
// needs no subprocess and no iproute2 in the image.
//
// IPv4 only. /proc/net/ipv6_route is a different and considerably fiddlier
// format, and a v6 default gateway is not a case this feature has to cover to
// work: a v6-only network still hands out a v4-mapped or v6 resolver through
// resolv.conf when DHCP DNS is not suppressed, and where it IS suppressed the
// v4 gateway is present too. Probing means a candidate that cannot answer is
// dropped rather than trusted, so the cost of the gap is a missing candidate,
// never a wrong one.
const hostRouteTablePath = "/proc/net/route"

// Route flags from linux/route.h. A default route is only a usable gateway when
// it is both up and actually via a gateway — a directly-attached default route
// has no next hop to send a query to.
const (
	routeFlagUp      = 0x1
	routeFlagGateway = 0x2
)

// DefaultForwarderProbeName is the name resolved through a candidate forwarder
// to prove it works.
//
// Reserved by RFC 2606 for exactly this kind of use, resolvable everywhere, and
// not owned by anyone whose outage would look like a broken network.
const DefaultForwarderProbeName = "example.com"

// DefaultForwarderProbeTimeout bounds one round of candidate probing.
//
// Candidates are probed concurrently, so this is the wall-clock ceiling for the
// whole round rather than a per-candidate budget. It is generous for a resolver
// on the local segment (the gateway answers in single-digit milliseconds) and
// short enough that a round which finds nothing does not stall the caller: the
// reprogramming tick and GET /dns/status both go through here.
const DefaultForwarderProbeTimeout = 3 * time.Second

// DefaultForwarderProbeTTL is how long a validated candidate list is reused
// before the probes are run again.
//
// Without it every ProgramRolodex tick would re-probe, which on a box where
// nothing answers means paying DefaultForwarderProbeTimeout on a schedule
// forever. The list only changes when the network does, so it is cached rather
// than recomputed; SetLocalForwarders drops the cache so an operator toggling
// the setting gets a fresh answer instead of a stale one.
const DefaultForwarderProbeTTL = 5 * time.Minute

// GatewaysFrom returns the default gateways named by a /proc/net/route-format
// file, as "host:port" forwarder strings. See hostRouteTablePath.
func GatewaysFrom(path string) []string {
	data, err := os.ReadFile(path) //nolint:gosec // G304 -- a system path, or a test fixture set through rolodex.Config
	if err != nil {
		return nil
	}
	return parseRouteTable(data)
}

// parseRouteTable extracts default-gateway addresses from /proc/net/route
// bytes, in file order, de-duplicated, each with the default DNS port applied.
//
// Columns are Iface, Destination, Gateway, Flags, and more this does not use. A
// default route is Destination 00000000; the header line is skipped by the same
// test, since its second column is the literal "Destination". Both the
// destination and the gateway are printed as native-endian hex, which on every
// architecture Town OS builds for means the address bytes come out reversed —
// hence the little-endian unpack rather than net.IP(binary.BigEndian...).
func parseRouteTable(data []byte) []string {
	var addrs []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&routeFlagUp == 0 || flags&routeFlagGateway == 0 {
			continue
		}
		raw, err := hex.DecodeString(fields[2])
		if err != nil || len(raw) != net.IPv4len {
			continue
		}
		ip := net.IPv4(raw[3], raw[2], raw[1], raw[0])
		if !usableForwarder(ip) {
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

// ForwarderDiscovery finds the forwarder addresses this box's own network can
// actually resolve through.
//
// Discovery alone is not enough, which is the lesson of the state this replaced:
// a candidate list assembled from configuration is a list of addresses somebody
// BELIEVES are resolvers, and on a filtered network the belief is exactly what
// is wrong. rolodex's "auto" chain reaches its forwarder tier only after the
// roots and the encrypted upstreams have failed, so a dead address there is not
// a harmless spare — it is a tier that cannot answer, sitting in front of the
// SERVFAIL, charging every query the full per-forwarder timeout on the way past.
// Probing is what makes the list a list of resolvers rather than of addresses.
//
// The zero value works: it reads the system paths and probes for real. Tests
// set Probe (and the two path fields) so discovery is a property of the test
// rather than of whatever network the machine running it happens to be on.
type ForwarderDiscovery struct {
	// ResolvConfPaths overrides the resolv.conf files read, in order. Empty
	// means hostResolvConfPaths.
	ResolvConfPaths []string
	// RouteTablePath overrides the routing table read for the default
	// gateway. Empty means hostRouteTablePath.
	RouteTablePath string
	// ProbeName overrides the name resolved to validate a candidate. Empty
	// means DefaultForwarderProbeName.
	ProbeName string
	// ProbeTimeout overrides the ceiling on one round of probing. Zero means
	// DefaultForwarderProbeTimeout.
	ProbeTimeout time.Duration
	// Probe overrides how a candidate is validated. Nil means a real DNS
	// query. A test that set this to nil by accident would reach the network,
	// so the injection point is the field rather than a build tag.
	Probe func(ctx context.Context, addr string) bool
}

// Candidates returns every address discovery is willing to consider, in
// preference order and de-duplicated, before any of them has been proven to
// work.
//
// resolv.conf comes first because it is what the network explicitly told this
// box to use, and the gateway is an inference from the route table — a good
// inference, and the only one available where DHCP DNS has been suppressed, but
// still the weaker claim of the two. Where both are present and both answer,
// the explicit one leads.
func (d ForwarderDiscovery) Candidates() []string {
	paths := d.ResolvConfPaths
	if len(paths) == 0 {
		paths = hostResolvConfPaths
	}
	routes := d.RouteTablePath
	if routes == "" {
		routes = hostRouteTablePath
	}

	var addrs []string
	seen := make(map[string]bool)
	for _, addr := range append(HostResolversFrom(paths...), GatewaysFrom(routes)...) {
		if seen[addr] {
			continue
		}
		seen[addr] = true
		addrs = append(addrs, addr)
	}
	return addrs
}

// Validated returns the candidates that answered a real query, in candidate
// order.
//
// Every candidate is probed, not just enough of them to find one winner: the
// forwarder list is rolodex's whole local tier, and a second working address in
// it is the difference between one resolver's outage being invisible and being
// an outage. They are probed concurrently because they are independent and the
// slowest is usually the one that is never going to answer.
//
// An empty result means nothing usable was found, and the caller keeps whatever
// forwarders it already had — see Manager.forwarders. That is deliberately the
// same contract as discovery finding no addresses at all, because to the tier
// being configured the two are the same situation.
func (d ForwarderDiscovery) Validated(ctx context.Context) []string {
	candidates := d.Candidates()
	if len(candidates) == 0 {
		return nil
	}

	probe := d.Probe
	if probe == nil {
		name := d.ProbeName
		if name == "" {
			name = DefaultForwarderProbeName
		}
		probe = func(ctx context.Context, addr string) bool {
			return probeForwarder(ctx, addr, name)
		}
	}

	timeout := d.ProbeTimeout
	if timeout <= 0 {
		timeout = DefaultForwarderProbeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ok := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for i, addr := range candidates {
		wg.Go(func() { ok[i] = probe(ctx, addr) })
	}
	wg.Wait()

	var working []string
	for i, addr := range candidates {
		if ok[i] {
			working = append(working, addr)
		}
	}
	return working
}

// probeForwarder reports whether addr answers a DNS query for name.
//
// The bar is a resolved name, not a reachable socket. A TCP connect or an ICMP
// reply proves the address is up, which on the networks this exists for is true
// of plenty of things that will not resolve anything — and a UDP :53 that is
// being black-holed by a filter is indistinguishable from a working one until
// something waits for an answer that never comes. Requiring at least one
// address back is what separates "there is a resolver here" from "there is
// something here".
//
// PreferGo forces Go's own resolver so Dial is honoured; without it cgo's
// resolver would ignore the address entirely and quietly probe whatever
// /etc/resolv.conf names, which on this box is the stub and would pass every
// candidate.
func probeForwarder(ctx context.Context, addr, name string) bool {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}
	found, err := resolver.LookupHost(ctx, name)
	return err == nil && len(found) > 0
}
