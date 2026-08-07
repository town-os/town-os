// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
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
		// A hostname is pasted into a site address, and a backend into a
		// reverse_proxy directive, so neither may carry Caddyfile syntax. Two
		// distinct failures follow if they do: the obvious one is that a
		// newline or a brace closes the block early and everything after it is
		// parsed as configuration; the one that actually bites is that caddy
		// validates the file as a whole, so ONE malformed entry makes `caddy
		// reload` reject it and every route on the box silently keeps serving
		// the last-good set. Whitespace is refused for a third reason: caddy
		// reads space-separated site addresses as several addresses for one
		// block, so "a.example.com b.example.com" claims a hostname that
		// belongs to somebody else.
		//
		// The bad route is dropped rather than failing the render, for the same
		// reason dedupeIngressRoutes drops a duplicate: the alternative is that
		// one page's domain field unpublishes every other service.
		if !validSiteHost(host) {
			slog.Error("ingress: dropping a route whose hostname is not a hostname; "+
				"it would restructure the Caddyfile or make caddy reject the whole config",
				"hostname", strconv.Quote(host))
			continue
		}
		if !validBackend(r.GetBackend()) {
			slog.Error("ingress: dropping a route whose backend is not a host:port",
				"hostname", host, "backend", strconv.Quote(r.GetBackend()))
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

// validSiteHost reports whether a hostname can be used as a Caddy site address.
//
// Letters, digits, dashes and dots only, in DNS-label shape. Deliberately
// narrower than "no Caddyfile metacharacters": Town OS composes every hostname
// it programs from a package FQDN or a page domain, both of which are DNS
// names, so anything else is a bug upstream rather than a shape somebody
// needs. A leading "*." is accepted because a wildcard site address is
// legitimate Caddy and costs nothing to allow.
func validSiteHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	host = strings.TrimPrefix(host, "*.")
	if host == "" {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !validHostLabel(label) {
			return false
		}
	}
	return true
}

// validHostLabel reports whether one dot-separated component is a DNS label:
// letters, digits and dashes, not starting or ending with a dash.
//
// No underscore. It is not a hostname character (RFC 1123), a certificate SAN
// carrying one is not a valid dNSName, and a vhost Town OS cannot issue a
// trusted leaf for is a vhost that fails the handshake rather than one that
// serves. Refusing it here keeps the ingress agreeing with what the CA will
// sign.
func validHostLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := range len(label) {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

// validBackend reports whether a backend is a bare host:port that can follow
// reverse_proxy.
//
// Backends are container names or addresses this code composes, never operator
// input, so this is a guard against a future caller rather than a live hole —
// but it is the same directive line, and leaving one half checked and the other
// not is how the checked half stops being true.
func validBackend(backend string) bool {
	if backend == "" || strings.ContainsAny(backend, " \t\n\r\x00{}#") {
		return false
	}
	host, port, found := strings.Cut(backend, ":")
	if !found || host == "" || port == "" {
		return false
	}
	if net.ParseIP(strings.Trim(host, "[]")) == nil && !validSiteHost(host) {
		return false
	}
	for i := range len(port) {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	return true
}
