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

// pageHostname returns the hostname a page assigned `domain` is served at under
// the rolodex TLD. A bare label (e.g. "blog") becomes "blog.<tld>"; a name that
// already ends in the TLD is used verbatim; a public FQDN (e.g. blog.example.com)
// is used verbatim and served via ACME rather than the local CA. Returns "" for
// an empty domain.
func pageHostname(domain, tld string) string {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if domain == "" {
		return ""
	}
	if tld == "" || isPublicFQDN(domain, tld) {
		return domain
	}
	if strings.HasSuffix(domain, "."+tld) || domain == tld {
		return domain
	}
	return domain + "." + tld
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
func issuePageLeaf(ca *townostls.CA, btrfsBase, pageName, hostname, internalIP string) (string, error) {
	if ca == nil || btrfsBase == "" || hostname == "" {
		return "", nil
	}
	// Add the host's global IPv6 SAN (paired to the internalIP interface) so a
	// direct https://[v6-literal] dial matches; empty/no-op on v4-only hosts.
	// Pages are home/LAN-only and never live on a WireGuard network, so they
	// carry no overlay SAN.
	_, internalIPv6 := InternalInterfaceIPs()
	sans := collectTLSSans(hostname, nil, internalIP, internalIPv6, "")
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

// collectPageHostnames returns the internal page hostnames (e.g. blog.<tld>)
// that must resolve to the host's internal IP. Public-FQDN pages are excluded —
// they resolve via the user's own DNS, like a package's public domains. Returns
// deduplicated, never-empty hostnames.
func collectPageHostnames(mgr account.PagesManager, tld string) []string {
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

// collectPageTLSA returns DANE TLSA entries for every internal page's leaf,
// used by RebuildDNS to re-pin page certs after a zone teardown.
func collectPageTLSA(mgr account.PagesManager, btrfsBase, tld string) []rolodex.TLSAEntry {
	if mgr == nil || btrfsBase == "" {
		return nil
	}
	list, err := mgr.List()
	if err != nil {
		return nil
	}
	var entries []rolodex.TLSAEntry
	for _, p := range list {
		domain := pageDomain(p)
		if pageIsPublic(domain, tld) {
			continue
		}
		host := pageHostname(domain, tld)
		entries = append(entries, buildPageTLSAEntries(btrfsBase, p.Name, host)...)
	}
	return entries
}
