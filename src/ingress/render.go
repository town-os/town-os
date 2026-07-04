// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"fmt"
	"sort"
	"strings"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// renderCaddyfile renders the shared ingress Caddyfile for the given routes.
// Each route gets an `https://<hostname>` vhost terminating TLS with the route's
// file-pinned local-CA leaf (or an explicit ACME issuer for a public FQDN) and
// reverse-proxying to the backend container on the ingress network. Each route
// also gets an `http://<hostname>` vhost on :80: pages (ServeHttp) are served
// over plain HTTP directly (static content, nothing sensitive), while packages
// get an HTTP->HTTPS redirect so they stay HTTPS-only. defaultBackend, when set,
// is the fallback for any host not matched by a route on :80 — the Town OS UI,
// so `http://<box-ip>/` (bare-IP login) keeps working now that the UI no longer
// squats the host's :80 directly.
//
// Output is sorted by hostname so the bytes are deterministic across reconciles
// — that is what lets the caddy supervisor no-op a Reload whose content has not
// changed. A route with no issued leaf yet (non-ACME, empty cert dir) is skipped
// for HTTPS so a half-provisioned entry never makes caddy reject the whole
// config; a ServeHttp page still gets its :80 vhost (no cert needed). Globals:
// auto_https off (we manage certs) and h1 h2 only (the ingress publishes TCP
// only, so H3/QUIC over UDP is unreachable). The admin API is left enabled (the
// default localhost:2019, container-local and unpublished) — the supervisor
// programs new routes with `caddy reload`, which talks to that endpoint, so
// `admin off` would break every route update after the first boot.
//
// httpsPort/httpPort are the TCP ports the vhosts bind. Production uses 443/80
// (rendered as bare `https://host`/`http://host`); tests pass ephemeral ports
// (rendered as `host:PORT`) so make test-full never collides on a privileged
// port.
func renderCaddyfile(routes []*ingresspb.Route, httpsPort, httpPort int, defaultBackend string) []byte {
	sorted := append([]*ingresspb.Route(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GetHostname() < sorted[j].GetHostname()
	})

	var b strings.Builder
	b.WriteString("{\n\tauto_https off\n\tservers {\n\t\tprotocols h1 h2\n\t}\n}\n")
	for _, r := range sorted {
		host := r.GetHostname()
		if host == "" || r.GetBackend() == "" {
			continue
		}
		httpsReady := r.GetAcme() || r.GetCertDir() != ""
		if httpsReady {
			fmt.Fprintf(&b, "\nhttps://%s {\n", siteAddr(host, httpsPort, 443))
			if r.GetAcme() {
				b.WriteString("\ttls {\n\t\tissuer acme\n\t}\n")
			} else {
				cd := r.GetCertDir()
				fmt.Fprintf(&b, "\ttls %s/cert.pem %s/key.pem\n", cd, cd)
			}
			writeReverseProxy(&b, r)
			b.WriteString("}\n")
		}
		// :80 vhost. Pages serve over plain HTTP; packages redirect to HTTPS
		// (only once the HTTPS target actually exists, so we never redirect into
		// a not-yet-provisioned cert).
		switch {
		case r.GetServeHttp():
			fmt.Fprintf(&b, "\nhttp://%s {\n", siteAddr(host, httpPort, 80))
			writeReverseProxy(&b, r)
			b.WriteString("}\n")
		case httpsReady:
			fmt.Fprintf(&b, "\nhttp://%s {\n\tredir https://%s{uri} permanent\n}\n",
				siteAddr(host, httpPort, 80), siteAddr(host, httpsPort, 443))
		}
	}
	// Default :80 backend (the UI): a bare port block catches every host not
	// matched by a route above. Rendered last; its content is host-independent
	// so it never breaks deterministic ordering.
	if defaultBackend != "" {
		fmt.Fprintf(&b, "\n:%d {\n\treverse_proxy %s\n}\n", httpPortOrDefault(httpPort), defaultBackend)
	}
	return []byte(b.String())
}

// writeReverseProxy emits the reverse_proxy directive for a route, proxying over
// https with verification skipped when the backend itself speaks TLS.
func writeReverseProxy(b *strings.Builder, r *ingresspb.Route) {
	if r.GetBackendTls() {
		// Backend serves HTTPS (e.g. a self-signed admin UI): proxy over https
		// and skip verification on the internal hop — the browser still
		// validates the ingress's trusted leaf.
		fmt.Fprintf(b, "\treverse_proxy https://%s {\n\t\ttransport http {\n\t\t\ttls_insecure_skip_verify\n\t\t}\n\t}\n", r.GetBackend())
		return
	}
	fmt.Fprintf(b, "\treverse_proxy %s\n", r.GetBackend())
}

// siteAddr returns the Caddy site address for a host on the given port. The
// scheme's default port (defaultPort, or 0) renders as a bare host so Caddy uses
// its scheme default; any other port is appended explicitly for test isolation.
func siteAddr(host string, port, defaultPort int) string {
	if port == 0 || port == defaultPort {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// httpPortOrDefault maps 0 to the standard HTTP port 80 for the bare-port
// default block (Caddy needs a concrete port there).
func httpPortOrDefault(httpPort int) int {
	if httpPort == 0 {
		return 80
	}
	return httpPort
}
