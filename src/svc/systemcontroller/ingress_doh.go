package systemcontroller

import (
	"context"
	"log/slog"

	upstream "gitea.com/town-os/rolodex-dns/go"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

const (
	// DohLeafRepo and DohLeafVersion place the DoH vhost's leaf alongside the
	// pages and gfeh leaves, under its own repo so reissuing one never disturbs
	// the others.
	DohLeafRepo = "doh"
	// DohLeafName is the leaf's name within that repo. The vhost is a single
	// fixed name, so there is one leaf and no per-object naming.
	DohLeafName = "resolver"
	// DohLeafVersion matches the other leaf repos: one live version, replaced in
	// place on reissue.
	DohLeafVersion = "current"

	// DohHostLabel is the label the DoH endpoint is served under, giving
	// clients a URL of https://dns.<tld>/dns-query.
	DohHostLabel = "dns"

	// RolodexDohBackend is where rolodex's own DoH listener lives: loopback, on
	// a port nothing else claims.
	//
	// This constant and `doh.bind` in ../install's scripts/rolodex-config.sh are
	// the same number written in two repos, and neither can read the other. It
	// is 4443 rather than 443 because the ingress is published on 0.0.0.0:443
	// and rolodex runs --net host: a wildcard :443 and a specific 127.0.0.2:443
	// in one namespace is EADDRINUSE for whichever binds second, which would
	// take out the ingress or DNS depending on boot order.
	RolodexDohBackend = "127.0.0.2:4443"
)

// dohIngressHostname is the name the DoH endpoint is served under.
func dohIngressHostname(tld string) string {
	if tld == "" {
		return ""
	}
	return DohHostLabel + "." + tld
}

// dohRecordName is the zone-qualified name the DoH endpoint answers under —
// the vhost hostname with the trailing dot rolodex records carry. Empty when
// there is no TLD to name it after, which both DNS reconcilers read as "publish
// nothing" rather than as a record for a bare "dns.".
func dohRecordName(tld string) string {
	hostname := dohIngressHostname(tld)
	if hostname == "" {
		return ""
	}
	return hostname + "."
}

// publishDohRecord registers the address records for the DoH endpoint's name.
//
// The vhost is only half of a resolver a client can use. dohIngressRoute gives
// dns.<tld> a Caddy site and a leaf from the box's CA, and a client that cannot
// resolve that name never reaches either — the failure lands before TLS, as
// NXDOMAIN, which reads like the feature was never built. Packages, pages and
// object storage each pair their vhost with an AddRecord call; this is DoH's
// half of the same pairing.
//
// Both families are published for the same reason the pages records are: the
// leaf carries the box's v4 and v6 addresses as SANs, so a client that reaches
// the ingress over either one still matches the certificate.
//
// Best-effort, like every other record publisher here: an error on this one
// name must not abort the rebuild that publishes every package's.
func publishDohRecord(ctx context.Context, cl rolodex.Client, tld, internalIP, internalIPv6 string) {
	name := dohRecordName(tld)
	if cl == nil || name == "" {
		return
	}
	if internalIP != "" {
		if err := cl.AddRecord(ctx, &upstream.DnsRecord{
			Name:       name,
			RecordType: upstream.RecordTypeA,
			Value:      internalIP,
			Ttl:        300,
		}); err != nil {
			slog.Debug("dns: DoH A record", "name", name, "error", err)
		}
	}
	if internalIPv6 != "" {
		if err := cl.AddRecord(ctx, &upstream.DnsRecord{
			Name:       name,
			RecordType: upstream.RecordTypeAAAA,
			Value:      internalIPv6,
			Ttl:        300,
		}); err != nil {
			slog.Debug("dns: DoH AAAA record", "name", name, "error", err)
		}
	}
}

// dohIngressRoute is the vhost that turns rolodex's loopback DoH listener into
// a DoH resolver clients can actually use.
//
// The ingress is what makes this worth doing. rolodex terminates its own TLS
// with a self-signed certificate, which every validating DoH client refuses; the
// ingress holds a leaf from the box's CA for this name and proxies the internal
// hop with verification skipped. So the certificate a client checks is the
// ingress's, and rolodex's is a detail of a hop that never leaves the box.
//
// The whole vhost proxies to rolodex rather than only /dns-query. rolodex's DoH
// router serves that one path and 404s everything else, so a path matcher here
// would add a second place for the path to be wrong without changing what an
// ordinary client sees.
//
// Returns nil when there is no CA or no TLD to name the vhost after, or when the
// leaf cannot be issued — the same rule the pages routes follow, and for the
// same reason: a route with an empty cert dir makes Caddy reject the whole
// config, which would take every other route on the box down with it.
func dohIngressRoute(ca *townostls.CA, btrfsBase, tld, internalIP string) *ingresspb.Route {
	hostname := dohIngressHostname(tld)
	if ca == nil || btrfsBase == "" || hostname == "" {
		return nil
	}

	// The box's global IPv6 SAN pairs with internalIP the way the pages leaves
	// do, so a client that reaches the ingress over v6 still matches.
	_, internalIPv6 := InternalInterfaceIPs()
	sans := collectTLSSans(hostname, nil, internalIP, internalIPv6, "")

	hostDir := hostTLSLeafDir(btrfsBase, DohLeafRepo, DohLeafName, DohLeafVersion)
	if err := ca.IssueLeaf(hostDir, sans); err != nil {
		slog.Debug("ingress: DoH leaf", "hostname", hostname, "error", err)
		return nil
	}

	return &ingresspb.Route{
		Hostname:   hostname,
		Backend:    RolodexDohBackend,
		CertDir:    containerTLSLeafDir(DohLeafRepo, DohLeafName, DohLeafVersion),
		BackendTls: true,
	}
}
