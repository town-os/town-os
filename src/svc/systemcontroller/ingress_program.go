// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/ingress"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

const (
	// PagesServiceKey is the system-service key for the standalone pages static
	// file server. Its container is town-os-system--pages, which the shared
	// ingress reverse-proxies to for every page FQDN.
	PagesServiceKey = "pages"
	// pagesBackendPort is the HTTP port the pages container serves on.
	pagesBackendPort = "80"
)

// pagesBackend is the ingress reverse_proxy target for page routes:
// town-os-system--pages:80 on the shared ingress network.
func pagesBackend() string {
	return systemd.SystemServiceContainerName(PagesServiceKey) + ":" + pagesBackendPort
}

// buildIngressRoutes assembles the full desired ingress route set: one route
// per HTTP package (reverse-proxied to its service container, reusing
// collectPackageIngressSites) and one per page (reverse-proxied to the shared
// pages service, with a freshly issued local-CA leaf for internal pages or an
// ACME-managed cert for public FQDNs). It mirrors the data the legacy
// file-mounted Caddyfile carried, but as gRPC Route messages the ingress
// renders itself.
//
// tld is the *global* dns_tld. It is the fallback for pre-upgrade package state
// files, and the TLD for pages on the DEFAULT network. Package vhost hostnames
// come from each package's state file FQDN (written under that package's
// install-network TLD — see collectPackageIngressSites); page vhost hostnames
// come from pageFQDN, which resolves the page's own network TLD via nm. A page
// on the "fart" network is served as blog.fart, with a leaf valid for that name
// and for the box's overlay address on that network.
func buildIngressRoutes(ctx context.Context, pagesMgr account.PagesManager, nm account.NetworkManager, installer FreshnessLister, gfehReg GfehRegistry, ca *townostls.CA, btrfsBase, stateDir, tld, internalIP string) []*ingresspb.Route {
	var routes []*ingresspb.Route

	for _, s := range collectPackageIngressSites(installer, stateDir, tld) {
		routes = append(routes, &ingresspb.Route{
			Hostname:   s.Hostname,
			Backend:    s.Backend,
			CertDir:    s.CertDir,
			Acme:       s.ACME,
			BackendTls: s.BackendTLS,
		})
	}

	routes = append(routes, gfehIngressRoutes(ctx, nm, gfehReg, ca, btrfsBase, tld, internalIP)...)

	if pagesMgr == nil {
		return dedupeIngressRoutes(routes)
	}
	pages, err := pagesMgr.List()
	if err != nil {
		slog.Debug(fmt.Sprintf("ingress: list pages: %v", err))
		return dedupeIngressRoutes(routes)
	}
	backend := pagesBackend()
	for _, page := range pages {
		domain := pageDomain(page)
		// The page's own network TLD, not the global dns_tld.
		pageTLD := pageNetworkTLD(nm, page.Network, tld)
		hostname := pageHostname(domain, pageTLD)
		if hostname == "" {
			continue
		}
		if pageIsPublic(domain, pageTLD) {
			// Public FQDN: served via ACME, resolved by the user's own DNS.
			// ServeHttp so a plain-HTTP visit is served directly (static content)
			// rather than redirected — pages carry nothing sensitive.
			routes = append(routes, &ingresspb.Route{Hostname: hostname, Backend: backend, Acme: true, ServeHttp: true})
			continue
		}
		certDir, lerr := issuePageLeaf(ca, btrfsBase, page.Name, hostname, internalIP, networkOverlayIPValue(nm, page.Network))
		if lerr != nil {
			slog.Debug(fmt.Sprintf("ingress: page leaf %s: %v", page.Name, lerr))
			continue
		}
		routes = append(routes, &ingresspb.Route{Hostname: hostname, Backend: backend, CertDir: certDir, ServeHttp: true})
	}
	return dedupeIngressRoutes(routes)
}

// gfehIngressRoutes turns each partition's HTTP views into ingress routes.
//
// SMB is excluded, and not as an oversight: it is not HTTP, so a vhost for it
// would complete a TLS handshake and then fail to speak the protocol — worse
// than no route, because the failure looks like a broken service rather than
// an absent one. SMB reaches its clients through its own host port and gets
// only a DNS record.
//
// ServeHttp is left false, so :80 redirects to HTTPS rather than serving
// directly. Pages set it true because static content carries nothing
// sensitive; a partition is somebody's files.
func gfehIngressRoutes(ctx context.Context, nm account.NetworkManager, reg GfehRegistry, ca *townostls.CA, btrfsBase, tld, internalIP string) []*ingresspb.Route {
	if reg == nil {
		return nil
	}

	// Every network's partition, not just the default one: the ingress is
	// interface-agnostic and selects a vhost purely by SNI, so one route set
	// serves LAN clients and overlay peers alike.
	var all []GfehSite
	all = append(all, collectGfehSites(ctx, reg, nm, tld, "")...)
	for network := range reg.Clients() {
		if gfeh.IsDefaultNetwork(network) {
			continue
		}
		all = append(all, collectGfehSites(ctx, reg, nm, tld, network)...)
	}

	// Render the index pages from exactly the site set these routes are built
	// from, on this same pass.
	//
	// Here rather than in ReconcileGfeh, and that placement is the point: this
	// function runs whenever the route set is rebuilt — boot, the hourly
	// reconcile, package and page CRUD, and critically publishGfehNames, which
	// is the first pass on a cold boot at which any daemon is answering at all
	// (gfehd polls /status/ping, which is 503 until the handler swap). An index
	// written from the gfeh reconcile would be written before the daemons could
	// say what they serve, and would sit stale until the next hour.
	//
	// It is I/O in a function named "build", which the leaf issuing above
	// already established: a route cannot be programmed before the bytes it
	// serves exist.
	reconcileGfehIndexes(btrfsBase, all)

	var routes []*ingresspb.Route
	issued := map[string]string{}
	for _, site := range all {
		if !site.HTTP || site.Backend == "" {
			continue
		}

		certDir, ok := issued[site.Network]
		if !ok {
			dir, err := issueGfehLeaf(ca, btrfsBase, site.Network,
				gfehHTTPFQDNs(all, site.Network),
				internalIP, networkOverlayIPValue(nm, site.Network))
			if err != nil {
				slog.Debug(fmt.Sprintf("ingress: gfeh leaf %s: %v", site.Network, err))
				issued[site.Network] = ""
				continue
			}
			issued[site.Network] = dir
			certDir = dir
		}
		if certDir == "" {
			// No leaf yet. Skipped rather than published without one: a route
			// with an empty cert dir makes Caddy reject the whole config, which
			// would take every other route on the box down with it.
			continue
		}

		routes = append(routes, &ingresspb.Route{
			Hostname: site.FQDN,
			Backend:  site.Backend,
			CertDir:  certDir,
		})
	}
	return routes
}

// dedupeIngressRoutes drops a route whose hostname a previous one already
// claimed, first wins.
//
// renderCaddyfile emits one vhost block per route with no de-duplication, and
// Caddy rejects a config with two blocks for the same hostname — taking down
// every route on the box, not just the duplicate. Packages and pages could
// already collide before object storage existed (a page named
// "gitea.default"); adding a third namespace makes it likelier. First wins
// because the caller appends in order of how user-visible the thing is:
// packages, then object storage, then pages.
func dedupeIngressRoutes(routes []*ingresspb.Route) []*ingresspb.Route {
	seen := make(map[string]bool, len(routes))
	out := make([]*ingresspb.Route, 0, len(routes))
	for _, r := range routes {
		if seen[r.GetHostname()] {
			slog.Warn("ingress: dropped a duplicate vhost; Caddy would refuse the whole config", "hostname", r.GetHostname(), "backend", r.GetBackend())
			continue
		}
		seen[r.GetHostname()] = true
		out = append(out, r)
	}
	return out
}

// RebuildIngress programs the ingress with the full desired route set (packages
// + pages). It is the declarative reconcile entry point — called at boot (the
// reconcile_ingress step), from the periodic reconcile, and after package
// install/uninstall and page CRUD — mirroring rolodex RebuildDNS.
func RebuildIngress(ctx context.Context, ic ingress.Client, pagesMgr account.PagesManager, nm account.NetworkManager, installer FreshnessLister, gfehReg GfehRegistry, ca *townostls.CA, btrfsBase, stateDir, tld, internalIP string) error {
	routes := buildIngressRoutes(ctx, pagesMgr, nm, installer, gfehReg, ca, btrfsBase, stateDir, tld, internalIP)
	if err := ic.SetRoutes(ctx, routes); err != nil {
		return fmt.Errorf("set ingress routes: %w", err)
	}
	return nil
}

// reprogramIngress rebuilds the ingress route set from the current installed
// packages and pages. Best-effort: errors are logged, not surfaced — the
// periodic reconcile is the backstop (same model as refreshPages/registerDNS).
func (s *SystemControllerHandlers) reprogramIngress(ctx context.Context) {
	ic := s.Controller.GetIngressClient()
	if ic == nil {
		return
	}
	tld := reconcileDNSTLD(ctx, s.Controller.GetSettingsManager())
	if err := RebuildIngress(ctx, ic, s.Controller.GetPagesManager(), s.Controller.GetNetworkManager(),
		s.Controller.GetInstaller(), s.Controller.GetGfehRegistry(),
		s.Controller.GetTLSCA(), s.Controller.GetBtrfsBasePath(), s.Controller.GetNetworkStatePath(),
		tld, s.Controller.GetInternalIP()); err != nil {
		slog.Debug(fmt.Sprintf("reprogram ingress: %v", err))
	}
}
