// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"path/filepath"
	"strings"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// PagesLeafRepo is the pseudo-repo segment under which a page's leaf cert is
// stored on disk: <btrfs>/tls/leaves/pages/<name>/<PagesLeafVersion>/. It keeps
// page leaves in the same tree the network controller already bind-mounts, so a
// single mount serves both package and page certs.
const PagesLeafRepo = "pages"

// PagesLeafVersion is the fixed version segment for a page's leaf dir. Pages are
// unversioned, so a constant keeps the path stable across reconciles (matching
// the per-package leaf layout, which keys on the package version).
const PagesLeafVersion = "current"

// PagesHTTPSPort is the port the pages Caddy terminates TLS on. Page hostnames
// are served at the implicit https default, so DANE TLSA records are published
// under _443._tcp.<hostname>.
const PagesHTTPSPort uint16 = 443

// pageDomain returns the effective domain for a page: its assigned Domain, or
// its Name when Domain is blank (mirroring controller_pages.go's create-time
// default so reconcile and the API agree on the served name).
func pageDomain(p account.PageSite) string {
	d := strings.TrimSpace(p.Domain)
	if d == "" {
		return p.Name
	}
	return d
}

// pageNetworkTLD returns the TLD a page's names are published under: its
// network's TLD, or globalTLD (the dns_tld setting) for a page on the default
// network. It is the page-side twin of networkTLDValue — a page on the "fart"
// network is blog.fart, never blog.home — and takes globalTLD rather than a
// SettingsManager because every page call site already has the global TLD in
// hand. An empty/unknown network, or a network with no TLD, falls back to
// globalTLD.
func pageNetworkTLD(nm account.NetworkManager, network, globalTLD string) string {
	if network == "" || network == account.DefaultNetworkName || nm == nil {
		return globalTLD
	}
	n, err := nm.Get(network)
	if err != nil || n.TLD == "" {
		return globalTLD
	}
	return n.TLD
}

// pageFQDN is the single source of truth for a page's name, the way packageFQDN
// is for a package's. It is the one string that the page's DNS record, its leaf
// cert SAN, its DANE TLSA owner, its ingress vhost, AND its on-disk subvolume /
// webroot symlink (the pages Caddy roots on /srv/<host>) must all agree on.
// Deriving all of them from this function is what stops them drifting apart.
func pageFQDN(nm account.NetworkManager, p account.PageSite, globalTLD string) string {
	return pageHostname(pageDomain(p), pageNetworkTLD(nm, p.Network, globalTLD))
}

// pageOnDefaultNetwork reports whether a page belongs to the default/home
// network, and so lives in the global home zone rather than a network's scoped
// TLD. Mirrors the exclusion collectInstalledDNSInfo applies to packages.
func pageOnDefaultNetwork(p account.PageSite) bool {
	return pageOnDefaultNetworkName(p.Network)
}

// pageHostname returns the hostname a page assigned `domain` is served at under
// the rolodex TLD. A bare label (e.g. "blog") becomes "blog.<tld>"; a name that
// already ends in the TLD is used verbatim; a public FQDN (e.g. blog.example.com)
// is used verbatim and served via ACME rather than the local CA. Returns "" for
// an empty domain.
//
// A public FQDN is returned verbatim and deliberately NOT put through
// qualifyPublishedName: it is the operator's own domain, resolved by their own
// DNS and served via ACME, so Town OS neither composes it nor gets to refuse
// it. Everything else — the internal names that become a rolodex record, a
// local-CA SAN, a TLSA owner, an ingress vhost, and a directory under
// /srv — is validated, because a page's `domain` is a text field an operator
// types and this is the one published name that is also a filesystem path.
func pageHostname(domain, tld string) string {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if domain == "" {
		return ""
	}
	if tld == "" || isPublicFQDN(domain, tld) {
		// Verbatim, but not unexamined. It is the operator's own domain, so
		// Town OS does not compose it under the box's TLD — but it still
		// becomes a Caddy vhost and a directory under pages-webroot/, and
		// isPublicFQDN reads anything containing a dot as public, which is how
		// `../escape.example.com` and `site.example.com/../../etc` reached
		// filepath.Join unchecked.
		return validatePublishedName("page", domain)
	}
	return qualifyPublishedName("page", domain, tld)
}

// pageIsPublic reports whether a page's domain resolves to a public FQDN (served
// via ACME, resolved by the user's own DNS) rather than an internal rolodex name
// (served with the local-CA leaf and pinned via DANE).
func pageIsPublic(domain, tld string) bool {
	return isPublicFQDN(strings.TrimSuffix(strings.TrimSpace(domain), "."), tld)
}

// issuePageLeaf issues (or refreshes) a leaf cert for an internal page hostname,
// returning the container-internal leaf dir for the Caddyfile's tls directive.
// Returns "" (no error) when ca is nil, btrfs is disabled, or the page is public
// (ACME manages the cert, so there is no file to pin).
//
// overlayIP is the box's WireGuard address on the page's network ("" for a
// default-network page, which has no WireGuard transport), so a peer on that
// network can reach the page by raw overlay address and not only by name.
func issuePageLeaf(ca *townostls.CA, btrfsBase, pageName, hostname, internalIP, overlayIP string) (string, error) {
	if ca == nil || btrfsBase == "" || hostname == "" {
		return "", nil
	}
	// Add the host's global IPv6 SAN (paired to the internalIP interface) so a
	// direct https://[v6-literal] dial matches; empty/no-op on v4-only hosts.
	_, internalIPv6 := InternalInterfaceIPs()
	sans := collectTLSSans(hostname, nil, internalIP, internalIPv6, overlayIP)
	hostDir := hostTLSLeafDir(btrfsBase, PagesLeafRepo, pageName, PagesLeafVersion)
	if err := ca.IssueLeaf(hostDir, sans); err != nil {
		return "", err
	}
	return containerTLSLeafDir(PagesLeafRepo, pageName, PagesLeafVersion), nil
}

// buildPageTLSAEntries returns the DANE TLSA entries pinning a page's leaf on
// :443 for its internal hostname. Returns nil with no error when the leaf cannot
// be read yet (callers treat a missing entry as "skip publishing").
func buildPageTLSAEntries(btrfsBase, pageName, hostname string) []rolodex.TLSAEntry {
	if btrfsBase == "" || hostname == "" {
		return nil
	}
	value, err := tlsaValue(filepath.Join(hostTLSLeafDir(btrfsBase, PagesLeafRepo, pageName, PagesLeafVersion), "cert.pem"))
	if err != nil || value == "" {
		return nil
	}
	return []rolodex.TLSAEntry{{Name: hostname, Port: PagesHTTPSPort, Value: value}}
}

// collectPageHostnames returns the internal hostnames (e.g. blog.<tld>) of pages
// on the DEFAULT network, which must resolve to the host's internal IP in the
// global home zone. Public-FQDN pages are excluded — they resolve via the user's
// own DNS, like a package's public domains. Non-default-network pages are also
// excluded: they live under their network's TLD and are dual-homed by
// RebuildNetworkDNS, exactly as collectInstalledDNSInfo excludes non-default
// packages. Including them here would republish them as .home AND make
// ReconcileDNS's remove pass fight the scoped registration. Returns
// deduplicated, never-empty hostnames.
func collectPageHostnames(mgr account.PagesManager, tld string) []string {
	return collectPageHostnamesForNetwork(mgr, "", tld, true)
}

// collectNetworkPageHostnames returns the internal hostnames of pages on the
// given NON-default network, named under that network's TLD.
func collectNetworkPageHostnames(mgr account.PagesManager, network, tld string) []string {
	return collectPageHostnamesForNetwork(mgr, network, tld, false)
}

// collectPageHostnamesForNetwork is the shared core: when defaultOnly is set it
// selects pages on the default network; otherwise it selects pages whose network
// matches `network`. tld is the TLD that selection is published under.
func collectPageHostnamesForNetwork(mgr account.PagesManager, network, tld string, defaultOnly bool) []string {
	if mgr == nil {
		return nil
	}
	list, err := mgr.List()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, p := range list {
		if defaultOnly != pageOnDefaultNetwork(p) {
			continue
		}
		if !defaultOnly && p.Network != network {
			continue
		}
		domain := pageDomain(p)
		if pageIsPublic(domain, tld) {
			continue
		}
		host := pageHostname(domain, tld)
		if host == "" {
			continue
		}
		if _, dup := seen[host]; dup {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	return out
}

// collectPageTLSA returns DANE TLSA entries for every default-network page's
// leaf, used by RebuildDNS to re-pin page certs after a zone teardown.
// Non-default-network pages are pinned under their own TLD by RebuildNetworkDNS.
func collectPageTLSA(mgr account.PagesManager, btrfsBase, tld string) []rolodex.TLSAEntry {
	return collectPageTLSAForNetwork(mgr, btrfsBase, "", tld, true)
}

// collectNetworkPageTLSA returns DANE TLSA entries for the pages on the given
// non-default network, pinned under that network's TLD.
func collectNetworkPageTLSA(mgr account.PagesManager, btrfsBase, network, tld string) []rolodex.TLSAEntry {
	return collectPageTLSAForNetwork(mgr, btrfsBase, network, tld, false)
}

func collectPageTLSAForNetwork(mgr account.PagesManager, btrfsBase, network, tld string, defaultOnly bool) []rolodex.TLSAEntry {
	if mgr == nil || btrfsBase == "" {
		return nil
	}
	list, err := mgr.List()
	if err != nil {
		return nil
	}
	var entries []rolodex.TLSAEntry
	for _, p := range list {
		if defaultOnly != pageOnDefaultNetwork(p) {
			continue
		}
		if !defaultOnly && p.Network != network {
			continue
		}
		domain := pageDomain(p)
		if pageIsPublic(domain, tld) {
			continue
		}
		host := pageHostname(domain, tld)
		entries = append(entries, buildPageTLSAEntries(btrfsBase, p.Name, host)...)
	}
	return entries
}
