// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// gfeh contributes hostnames; Town OS publishes them.
//
// The direction is inverted from every other integration, and deliberately so:
// RebuildDNS calls TeardownTLD and RebuildIngress calls SetRoutes with the full
// derived set, so both destroy anything they did not produce. A hostname gfeh
// registered itself would last exactly until the next reconcile. Instead gfeh
// answers GET /v1/names and Town OS folds the answer into what it is about to
// derive — which makes it impossible for gfeh to clobber a record RebuildDNS
// owns, because there is no code that writes one.
//
// This file is the Town OS half of that: ask, compose the zone, issue the cert,
// and hand the result to the DNS and ingress builders.

const (
	// GfehLeafRepo is the repo slot a partition's leaf cert is filed under, so
	// its directory sits beside package and page leaves rather than needing a
	// second layout.
	GfehLeafRepo = "gfeh"
	// GfehLeafVersion is the version slot. Partitions are not versioned, so
	// this is a constant rather than a number that would only ever be 1.
	GfehLeafVersion = "current"
)

// GfehSite is one view of one partition, resolved to the name Town OS serves it
// under.
type GfehSite struct {
	// Network is the Town OS network, and therefore the partition.
	Network string
	// View is s3, http, drive, ipfs or smb.
	View string
	// FQDN is the fully-qualified name: the one string this view's A record,
	// its leaf SAN, its DANE TLSA owner and its ingress vhost must all agree
	// on. gfeh supplies only the label; the zone is composed here.
	FQDN string
	// Port is what gfeh reported. For an HTTP view that is the container-side
	// backend port the ingress proxies to; for SMB it is the host port a
	// client dials directly. The asymmetry is confined to this field.
	Port uint16
	// Backend is "<container>:<port>" for an HTTP view, empty for SMB.
	Backend string
	// HTTP reports whether the ingress can front this view.
	HTTP bool
}

// gfehFQDN composes a view's fully-qualified name from the label gfeh reported.
//
// The third member of the packageFQDN / pageFQDN family, and the same rule
// applies: everything that names this view derives it from here, because four
// things that compose the same string separately are four things that drift.
//
// tld is the *partition network's* TLD, never the global dns_tld — a partition
// on the "office" network is s3.gfeh.office, not s3.gfeh.home.
//
// Composed directly rather than through pageHostname, which was the first
// attempt and was wrong for every single name. A page's domain may legitimately
// be a public FQDN, so pageHostname defers to isPublicFQDN, which classifies
// anything containing a dot and not ending in the TLD as public — and a gfeh
// label always contains a dot, because it is "<view>.gfeh". Every name came
// back unqualified and would have been served with an ACME cert for a
// nonexistent public domain instead of the local-CA leaf.
//
// There is no public-FQDN case here to get right: TOWNOS_CONTRACT.md is
// explicit that gfeh answers with a label and never an FQDN, precisely so the
// zone stays Town OS's to decide.
//
// This is also the chokepoint where a label stops being a string on a wire and
// becomes a vhost, a DNS record and a path — see the comment on
// gfeh.ValidateLabel. A label that does not validate yields the empty string,
// and every caller already drops an empty FQDN, so a malformed name contributes
// no record, no route, no certificate and no directory rather than contributing
// a broken one.
//
// Those rules now live in qualifyPublishedName, shared with packages and pages.
// They were written here first because gfeh reports its labels over a socket —
// but they turned out to be the rules all three publishers needed, and the
// other two had none.
func gfehFQDN(label, tld string) string {
	return qualifyPublishedName("gfeh", label, tld)
}

// gfehNetworkTLD resolves a partition's TLD. The page-side twin, reused rather
// than reimplemented.
func gfehNetworkTLD(nm account.NetworkManager, network, globalTLD string) string {
	return pageNetworkTLD(nm, network, globalTLD)
}

// gfehContainerName is the podman container a partition's views are served
// from, which is what the ingress proxies to on the shared network.
func gfehContainerName(key string) string {
	return systemd.SystemServiceContainerName(key)
}

// collectGfehSites asks every registered partition for its names.
//
// networkFilter selects which partitions contribute: the empty string means
// the default network only (whose names live in the global home zone), and a
// network name means that one. This mirrors the collectPageHostnames /
// collectNetworkPageHostnames split, and it exists because the two callers
// publish into different places — the global zone and a scoped one.
//
// A partition whose socket does not answer contributes nothing. gfehd answers
// /v1/names from its configuration rather than its listeners precisely so a
// reconcile racing a restart is still answered correctly, but a daemon that is
// genuinely down loses its records for this cycle. That is the right trade:
// the alternative is publishing an A record at a port nothing is listening on,
// which fails after a connection timeout and looks like a network fault rather
// than a stopped service.
func collectGfehSites(ctx context.Context, reg GfehRegistry, nm account.NetworkManager, globalTLD, networkFilter string) []GfehSite {
	if reg == nil {
		return nil
	}

	var sites []GfehSite
	for network, client := range reg.Clients() {
		if !gfehNetworkMatches(network, networkFilter) {
			continue
		}

		list, err := client.Names(ctx)
		if err != nil {
			slog.Debug("gfeh partition did not answer for its names", "network", network, "error", err)
			continue
		}

		tld := gfehNetworkTLD(nm, network, globalTLD)
		container := gfehContainerName(gfeh.ServiceKey(network))

		browsable := false
		for _, name := range list.Names {
			fqdn := gfehFQDN(name.Hostname, tld)
			if fqdn == "" {
				continue
			}
			site := GfehSite{
				Network: network,
				View:    name.View,
				FQDN:    fqdn,
				Port:    name.Port,
				HTTP:    gfeh.IsHTTPView(name.View),
			}
			if site.HTTP {
				site.Backend = fmt.Sprintf("%s:%d", container, name.Port)
				browsable = true
			}
			sites = append(sites, site)
		}

		// The index, published under the label every view label is a child of.
		//
		// Contributed here rather than by a parallel collector so it is carried
		// by the same six derivations the views are — A, AAAA, scoped record,
		// DANE pin, leaf SAN, ingress route — with no chance of the vhost and
		// the certificate being composed from different strings.
		//
		// Only when the partition has at least one view the ingress fronts. An
		// index for a partition serving nothing browsable would be a name in
		// DNS, a certificate and a route, all to render a page that says there
		// is nothing to see; and a partition whose daemon did not answer at all
		// has already contributed nothing above.
		if browsable {
			if site, ok := gfehIndexSite(network, tld); ok {
				sites = append(sites, site)
			}
		}
	}

	// Sorted so the derived route set and the rendered Caddyfile are stable
	// across reconciles — the registry is a map, and output that reordered
	// itself would make the ingress supervisor reload on every pass.
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].FQDN != sites[j].FQDN {
			return sites[i].FQDN < sites[j].FQDN
		}
		return sites[i].View < sites[j].View
	})
	return sites
}

// gfehNetworkMatches implements the default-vs-named selection.
func gfehNetworkMatches(network, filter string) bool {
	if filter == "" {
		return gfeh.IsDefaultNetwork(network)
	}
	return network == filter
}

// gfehHTTPFQDNs is the sorted set of names a partition's HTTP views are served
// under — the SAN set for its one leaf.
//
// Sorted, and that is load-bearing rather than tidiness: IssueLeaf is
// idempotent only when the requested SAN set matches what is on disk, so an
// order that varied between reconciles would re-issue the certificate every
// hour, changing its DANE pin and invalidating every TLSA record published for
// it.
func gfehHTTPFQDNs(sites []GfehSite, network string) []string {
	var out []string
	for _, s := range sites {
		if s.Network == network && s.HTTP {
			out = append(out, s.FQDN)
		}
	}
	sort.Strings(out)
	return out
}

// issueGfehLeaf issues one leaf per partition covering every HTTP view's name.
//
// One certificate rather than four: the views share a container and a client
// reaching any of them gets the same connection terminated by the same ingress,
// so four certificates would be four things to keep in step for no gain — and
// four DANE pins where one will do.
//
// overlayIP is the box's WireGuard address on the partition's network (empty
// for the default network, which has no transport), so a peer can reach the
// partition by raw overlay address and not only by name.
func issueGfehLeaf(ca *townostls.CA, btrfsBase, network string, fqdns []string, internalIP, overlayIP string) (string, error) {
	if ca == nil || btrfsBase == "" || len(fqdns) == 0 {
		return "", nil
	}

	_, internalIPv6 := InternalInterfaceIPs()
	sans := collectTLSSans(fqdns[0], fqdns[1:], internalIP, internalIPv6, overlayIP)

	hostDir := hostTLSLeafDir(btrfsBase, GfehLeafRepo, network, GfehLeafVersion)
	if err := ca.IssueLeaf(hostDir, sans); err != nil {
		return "", err
	}
	return containerTLSLeafDir(GfehLeafRepo, network, GfehLeafVersion), nil
}

// publishGfehGlobalRecords publishes A/AAAA for the default network's
// partition into the global zone, at the box's LAN address.
func publishGfehGlobalRecords(ctx context.Context, cfg ReconcileDNSConfig, sites []GfehSite) {
	if cfg.Client == nil {
		return
	}
	for _, site := range sites {
		addGfehAddressRecords(ctx, cfg, site.FQDN, cfg.InternalIP, cfg.InternalIPv6, "")
	}
}

// publishGfehNetworkRecords dual-homes a non-default network's partition: a
// scoped record at the overlay address for WireGuard peers, and a global one at
// the LAN address for loopback and LAN clients.
//
// Both, not either. A peer cannot route to the box's LAN address, and a LAN
// client cannot route to its overlay address, so publishing one would leave
// half the audience with a name that resolves to somewhere unreachable — which
// fails after a connection timeout and looks like a broken service.
func publishGfehNetworkRecords(ctx context.Context, cfg ReconcileDNSConfig, network, overlayIP string, sites []GfehSite) {
	if cfg.Client == nil {
		return
	}
	for _, site := range sites {
		addGfehAddressRecords(ctx, cfg, site.FQDN, cfg.InternalIP, cfg.InternalIPv6, "")
		if overlayIP == "" {
			continue
		}
		if err := cfg.Client.AddScopedRecord(ctx, network, &upstream.DnsRecord{
			Name: site.FQDN + ".", RecordType: upstream.RecordTypeA, Value: overlayIP, Ttl: 300,
		}); err != nil {
			slog.Debug("register scoped gfeh record", "fqdn", site.FQDN, "network", network, "error", err)
		}
	}
}

// addGfehAddressRecords publishes the global A and AAAA for one name.
func addGfehAddressRecords(ctx context.Context, cfg ReconcileDNSConfig, fqdn, internalIP, internalIPv6, _ string) {
	if internalIP != "" {
		if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
			Name: fqdn + ".", RecordType: upstream.RecordTypeA, Value: internalIP, Ttl: 300,
		}); err != nil {
			slog.Debug("register gfeh A record", "fqdn", fqdn, "error", err)
		}
	}
	if internalIPv6 != "" {
		if err := cfg.Client.AddRecord(ctx, &upstream.DnsRecord{
			Name: fqdn + ".", RecordType: upstream.RecordTypeAAAA, Value: internalIPv6, Ttl: 300,
		}); err != nil {
			slog.Debug("register gfeh AAAA record", "fqdn", fqdn, "error", err)
		}
	}
}

// collectGfehTLSA returns the DANE TLSA entries pinning each partition's leaf
// on :443, for the HTTP views only.
//
// SMB gets none: it is not TLS, and a TLSA record for a plain TCP service
// claims something untrue about it.
func collectGfehTLSA(sites []GfehSite, btrfsBase string) []rolodex.TLSAEntry {
	if btrfsBase == "" {
		return nil
	}

	// One pin value per partition — the views share a certificate — but one
	// record per name, because a TLSA record is owned by the name it pins.
	values := map[string]string{}
	var entries []rolodex.TLSAEntry

	for _, s := range sites {
		if !s.HTTP {
			continue
		}
		value, ok := values[s.Network]
		if !ok {
			certPath := filepath.Join(hostTLSLeafDir(btrfsBase, GfehLeafRepo, s.Network, GfehLeafVersion), "cert.pem")
			v, err := tlsaValue(certPath)
			if err != nil || v == "" {
				// A partition whose leaf has not been issued yet contributes
				// no pin. Publishing one for a certificate that does not exist
				// would make every client refuse the connection once it did.
				values[s.Network] = ""
				continue
			}
			values[s.Network] = v
			value = v
		}
		if value == "" {
			continue
		}
		entries = append(entries, rolodex.TLSAEntry{
			Name:  s.FQDN,
			Port:  PagesHTTPSPort,
			Value: value,
		})
	}
	return entries
}
