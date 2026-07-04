// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"

	"gitea.com/town-os/town-os/src/account"
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
func buildIngressRoutes(pagesMgr account.PagesManager, installer FreshnessLister, ca *townostls.CA, btrfsBase, stateDir, tld, internalIP string) []*ingresspb.Route {
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

	if pagesMgr == nil {
		return routes
	}
	pages, err := pagesMgr.List()
	if err != nil {
		slog.Debug(fmt.Sprintf("ingress: list pages: %v", err))
		return routes
	}
	backend := pagesBackend()
	for _, page := range pages {
		domain := pageDomain(page)
		hostname := pageHostname(domain, tld)
		if hostname == "" {
			continue
		}
		if pageIsPublic(domain, tld) {
			// Public FQDN: served via ACME, resolved by the user's own DNS.
			// ServeHttp so a plain-HTTP visit is served directly (static content)
			// rather than redirected — pages carry nothing sensitive.
			routes = append(routes, &ingresspb.Route{Hostname: hostname, Backend: backend, Acme: true, ServeHttp: true})
			continue
		}
		certDir, lerr := issuePageLeaf(ca, btrfsBase, page.Name, hostname, internalIP)
		if lerr != nil {
			slog.Debug(fmt.Sprintf("ingress: page leaf %s: %v", page.Name, lerr))
			continue
		}
		routes = append(routes, &ingresspb.Route{Hostname: hostname, Backend: backend, CertDir: certDir, ServeHttp: true})
	}
	return routes
}

// RebuildIngress programs the ingress with the full desired route set (packages
// + pages). It is the declarative reconcile entry point — called at boot (the
// reconcile_ingress step), from the periodic reconcile, and after package
// install/uninstall and page CRUD — mirroring rolodex RebuildDNS.
func RebuildIngress(ctx context.Context, ic ingress.Client, pagesMgr account.PagesManager, installer FreshnessLister, ca *townostls.CA, btrfsBase, stateDir, tld, internalIP string) error {
	routes := buildIngressRoutes(pagesMgr, installer, ca, btrfsBase, stateDir, tld, internalIP)
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
	tld := reconcileDNSTLD(s.Controller.GetSettingsManager())
	if err := RebuildIngress(ctx, ic, s.Controller.GetPagesManager(), s.Controller.GetInstaller(),
		s.Controller.GetTLSCA(), s.Controller.GetBtrfsBasePath(), s.Controller.GetNetworkStatePath(),
		tld, s.Controller.GetInternalIP()); err != nil {
		slog.Debug(fmt.Sprintf("reprogram ingress: %v", err))
	}
}
