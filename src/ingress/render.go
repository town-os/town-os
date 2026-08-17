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

	"gitea.com/town-os/town-os/src/i18n"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

const (
	// DefaultAdminPort is the port caddy's admin API binds when no override is
	// given, and caddy's own default. Production keeps it: the ingress container
	// has its own network namespace and publishes nothing but :443, :80 and the
	// loopback metrics port, so the admin API is reachable from inside that
	// container and nowhere else.
	//
	// Anything running caddy in the HOST namespace must override it —
	// see renderCaddyfileTally.
	DefaultAdminPort = 2019

	// adminHost is the interface the admin API binds. Loopback, and never
	// anything else: the admin API can rewrite the entire running config, so a
	// caddy that answered it on the LAN would let anyone who can reach the box
	// re-point every hostname it serves.
	adminHost = "127.0.0.1"
)

// renderCaddyfileTally renders the shared ingress Caddyfile for the given
// routes, along with a tally of what it refused to emit.
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
// auto_https off (we manage certs), h1 h2 only (the ingress publishes TCP only,
// so H3/QUIC over UDP is unreachable), and the admin endpoint. The admin API
// stays enabled — the supervisor programs new routes with `caddy reload`, which
// talks to that endpoint, so `admin off` would break every route update after
// the first boot.
//
// httpsPort/httpPort are the TCP ports the vhosts bind. Production uses 443/80
// (rendered as bare `https://host`/`http://host`); tests pass ephemeral ports
// (rendered as `host:PORT`) so make test-full never collides on a privileged
// port.
//
// adminPort is the same arrangement for the admin API, and it is written out
// rather than left to caddy's default for the reason the other two are: caddy
// defaults it to localhost:2019, which is fine for the production ingress
// (its own container, its own netns, nothing published) and is NOT fine for a
// test, which runs caddy in the host namespace — `go test ./src/...` runs on the
// host outright, and the integration container is `--net host`. Two concurrent
// `make test-full` runs would put two caddy children on the one :2019: the
// second fails to bind and exits, and before it does, either run's `caddy
// reload` can land on the other's admin API and program it with the wrong
// Caddyfile. Rendering the address here is also what points `caddy reload` at
// it, since that command reads the admin address out of the config it is
// adapting.
// The tally it returns counts what it refused to emit, by kind and reason. That
// exists because every drop below is a silent one from the caller's side: the
// route was accepted over gRPC, logged as dropped here, and then simply never
// served. A log line is the wrong instrument for that — nobody reads the
// ingress's journal until something is already known to be broken — so the
// counts are exported (townos_ingress_dropped_total) and can be alerted on.
//
// defaultBackendTLS marks that fallback as speaking HTTPS, so it is reached
// through writeReverseProxy like every other hop rather than being the one
// place in this renderer that could only send plaintext.
//
// Every proxy the file emits also carries the retry page (render_unavailable.go):
// a backend answering 5xx, or not answering at all, is served a page that says
// the service is unavailable and reloads itself until it is back, instead of
// caddy's bare 502. A home box restarts services constantly — an upgrade, a
// settings change, a reboot — and the honest state during those seconds is
// "coming back", which is not what a browser error page says.
//
// locale is the language that page falls back to when the client's own is one
// Town OS ships no catalog for — the box's configured locale, the same rule the
// UI follows. It is the `default` row of the language map; every language Town
// OS does ship gets a row of its own, matched against Accept-Language.
func renderCaddyfileTally(routes []*ingresspb.Route, httpsPort, httpPort, adminPort int, defaultBackend string, defaultBackendTLS bool, locale string) ([]byte, renderTally) {
	var tally renderTally
	sorted := append([]*ingresspb.Route(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GetHostname() < sorted[j].GetHostname()
	})

	var b strings.Builder
	b.WriteString("{\n\tauto_https off\n")
	fmt.Fprintf(&b, "\tadmin %s\n", adminAddr(adminPort))
	b.WriteString("\tservers {\n\t\tprotocols h1 h2\n\t}\n}\n")
	// The retry-page snippets, defined once and imported by every site block
	// that proxies anywhere. Unconditionally, even for an empty route set: a
	// snippet nothing imports is never parsed as directives, and emitting it on
	// a fixed line keeps the render deterministic — which is what lets the
	// supervisor no-op a reload whose bytes have not changed.
	b.WriteString(unavailableSnippets(locale))
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
			tally.drop(dropKindRoute, dropReasonHostname)
			continue
		}
		if !validBackend(r.GetBackend()) {
			slog.Error("ingress: dropping a route whose backend is not a host:port",
				"hostname", host, "backend", strconv.Quote(r.GetBackend()))
			tally.drop(dropKindRoute, dropReasonBackend)
			continue
		}
		// Validated once per route rather than once per vhost: a route that is
		// both HTTPS-ready and ServeHttp renders two site blocks from the same
		// path backends, and validating inside the writer would tally every
		// refused path twice — a drop counter that reports two of one mistake
		// is a counter nobody can read a magnitude off.
		paths := validPathBackends(r, &tally)
		httpsReady := r.GetAcme() || r.GetCertDir() != ""
		if httpsReady {
			fmt.Fprintf(&b, "\nhttps://%s {\n", siteAddr(host, httpsPort, 443))
			if r.GetAcme() {
				b.WriteString("\ttls {\n\t\tissuer acme\n\t}\n")
			} else {
				cd := r.GetCertDir()
				fmt.Fprintf(&b, "\ttls %s/cert.pem %s/key.pem\n", cd, cd)
			}
			writeRouteBody(&b, r, paths)
			writeUnavailableError(&b, host)
			b.WriteString("}\n")
		}
		// :80 vhost. Pages serve over plain HTTP; packages redirect to HTTPS
		// (only once the HTTPS target actually exists, so we never redirect into
		// a not-yet-provisioned cert).
		switch {
		case r.GetServeHttp():
			fmt.Fprintf(&b, "\nhttp://%s {\n", siteAddr(host, httpPort, 80))
			writeRouteBody(&b, r, paths)
			writeUnavailableError(&b, host)
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
		fmt.Fprintf(&b, "\n:%d {\n", httpPortOrDefault(httpPort))
		// Through writeReverseProxy like every other hop, so a fallback backend
		// that terminates its own TLS is reachable too. This was the last place
		// in the renderer that could only speak plaintext.
		writeReverseProxy(&b, "\t", defaultBackend, defaultBackendTLS)
		writeUnavailableError(&b, defaultBackendLabel)
		b.WriteString("}\n")
	}
	return []byte(b.String()), tally
}

// renderCaddyfile renders the Caddyfile and discards the tally of what was
// refused. The server uses renderCaddyfileTally, because a dropped route is
// exactly the kind of failure that should be a number an alert can watch; this
// plain form is for the render tests, which assert on the emitted bytes.
//
// The admin port is the default here, not a parameter, because no render test
// cares which one is emitted and threading it through every one of them would
// only obscure the port they DO assert on. The tests that need a real one are
// the ones that start a real caddy, and those go through NewServer. The
// fallback locale is the default for the same reason; the tests that care about
// a different one call renderCaddyfileTally.
func renderCaddyfile(routes []*ingresspb.Route, httpsPort, httpPort int, defaultBackend string, defaultBackendTLS bool) []byte {
	content, _ := renderCaddyfileTally(routes, httpsPort, httpPort, DefaultAdminPort, defaultBackend, defaultBackendTLS, i18n.DefaultLocale)
	return content
}

// adminAddr is the address caddy's admin API binds, and the one `caddy reload`
// dials to reach it.
//
// 127.0.0.1 rather than the localhost spelling caddy defaults to: localhost
// resolves to ::1 first on a dual-stack box, and caddy binds only one of the
// two, which is why a reload against a still-starting child logs
// `dial tcp [::1]:2019: connect: connection refused` rather than naming the
// address it actually tried. A literal leaves nothing to resolve. Caddy's
// default origin check accepts it: for a loopback listen address the allowed
// origins are localhost, ::1 and 127.0.0.1 at that same port.
func adminAddr(port int) string {
	if port == 0 {
		port = DefaultAdminPort
	}
	return net.JoinHostPort(adminHost, strconv.Itoa(port))
}

// Drop kinds and reasons, the two label values of townos_ingress_dropped_total.
// They are constants because they are what an alert selects on: a typo'd
// literal at one call site becomes its own permanent series that no query
// names.
const (
	dropKindRoute       = "route"
	dropKindPathBackend = "path_backend"

	dropReasonHostname  = "hostname"
	dropReasonBackend   = "backend"
	dropReasonPath      = "path"
	dropReasonDuplicate = "duplicate"
)

// renderTally counts the entries one render refused to emit, keyed by
// (kind, reason). The zero value is usable and allocates nothing, which is the
// common case: almost every render drops nothing at all.
type renderTally struct {
	dropped map[[2]string]uint64
}

// drop records one refused entry.
func (t *renderTally) drop(kind, reason string) {
	if t.dropped == nil {
		t.dropped = make(map[[2]string]uint64, 1)
	}
	t.dropped[[2]string{kind, reason}]++
}

// writeRouteBody emits the routing half of a site block.
//
// One reverse_proxy for the ordinary route — one vhost, one service — and a
// handle block per path-scoped backend when the route has any, with the route's
// own backend as the catch-all. paths is the already-validated path-backend set
// (validPathBackends), passed in rather than derived here so a route rendering
// two site blocks validates — and tallies — its paths exactly once.
//
// handle blocks are mutually exclusive and evaluated in the order written, so
// the bare one has to come last: it matches everything, and anything after it
// would be dead config. That ordering is also why the path backends are emitted
// in the order the caller supplied rather than sorted. Sorting would be safe for
// the matchers Town OS actually programs (they do not overlap) and would quietly
// change which one wins for a pair that did.
func writeRouteBody(b *strings.Builder, r *ingresspb.Route, paths []*ingresspb.PathBackend) {
	if len(paths) == 0 {
		writeReverseProxy(b, "\t", r.GetBackend(), r.GetBackendTls())
		return
	}
	for _, p := range paths {
		fmt.Fprintf(b, "\thandle %s {\n", p.GetPath())
		// Each path backend carries its own scheme. This used to be hardcoded
		// to plain HTTP on the reasoning that the only path backend Town OS
		// programmed — the pages container serving the object-storage index —
		// speaks :80 like every other page. That stopped being true the moment
		// a path had to reach something that terminates its own TLS (rolodex's
		// DoH listener), and a hardcoded scheme fails in the worst way: the
		// proxy sends plaintext at a TLS socket and the client gets a 502 with
		// nothing to say the config was the problem.
		writeReverseProxy(b, "\t\t", p.GetBackend(), p.GetBackendTls())
		b.WriteString("\t}\n")
	}
	b.WriteString("\thandle {\n")
	writeReverseProxy(b, "\t\t", r.GetBackend(), r.GetBackendTls())
	b.WriteString("\t}\n")
}

// writeReverseProxy emits one reverse_proxy directive at the given indent,
// proxying over https with verification skipped when the backend speaks TLS.
//
// Every proxy carries the retry-page import, which is why even the plain-HTTP
// form now opens a block: a backend answering 5xx is intercepted there, inside
// the proxy that received it, rather than at the site level where caddy cannot
// see an upstream response at all. What it does with that 5xx is raise a 503
// into the site's own error handler, so the page itself is written once per
// site block — see writeUnavailableResponse.
func writeReverseProxy(b *strings.Builder, indent, backend string, backendTLS bool) {
	if backendTLS {
		// Backend serves HTTPS (e.g. a self-signed admin UI): proxy over https
		// and skip verification on the internal hop — the browser still
		// validates the ingress's trusted leaf.
		fmt.Fprintf(b, "%sreverse_proxy https://%s {\n%s\ttransport http {\n%s\t\ttls_insecure_skip_verify\n%s\t}\n",
			indent, backend, indent, indent, indent)
	} else {
		fmt.Fprintf(b, "%sreverse_proxy %s {\n", indent, backend)
	}
	writeUnavailableResponse(b, indent+"\t")
	fmt.Fprintf(b, "%s}\n", indent)
}

// validPathBackends drops the path backends of a route that cannot safely be
// rendered, keeping the first of any duplicate path.
//
// Dropped individually rather than failing the route, and the difference
// matters: the route's own backend is the catch-all, so losing a path backend
// costs the one path it claimed while the service behind the name keeps
// answering. Failing the route would take the whole name off the air to fix a
// sub-path, and rendering it anyway would put unvalidated text inside a handle
// directive — where a brace does not break one path but makes caddy reject the
// entire config, taking every vhost on the box down with it.
//
// A duplicate path is dropped for a subtler reason than caddy refusing it: two
// handle blocks with the same matcher are accepted, and the second is simply
// unreachable. First wins, matching dedupeIngressRoutes.
func validPathBackends(r *ingresspb.Route, tally *renderTally) []*ingresspb.PathBackend {
	pbs := r.GetPathBackends()
	if len(pbs) == 0 {
		return nil
	}
	out := make([]*ingresspb.PathBackend, 0, len(pbs))
	seen := make(map[string]bool, len(pbs))
	for _, p := range pbs {
		if !validPathMatcher(p.GetPath()) {
			slog.Error("ingress: dropping a path backend whose path is not a path; "+
				"it would restructure the Caddyfile or make caddy reject the whole config",
				"hostname", r.GetHostname(), "path", strconv.Quote(p.GetPath()))
			tally.drop(dropKindPathBackend, dropReasonPath)
			continue
		}
		if !validBackend(p.GetBackend()) {
			slog.Error("ingress: dropping a path backend that is not a host:port",
				"hostname", r.GetHostname(), "path", p.GetPath(), "backend", strconv.Quote(p.GetBackend()))
			tally.drop(dropKindPathBackend, dropReasonBackend)
			continue
		}
		if seen[p.GetPath()] {
			slog.Warn("ingress: dropped a duplicate path backend; the second handle block would be unreachable",
				"hostname", r.GetHostname(), "path", p.GetPath())
			tally.drop(dropKindPathBackend, dropReasonDuplicate)
			continue
		}
		seen[p.GetPath()] = true
		out = append(out, p)
	}
	return out
}

// validPathMatcher reports whether a string can be pasted into a caddy handle
// directive as a path matcher.
//
// An absolute path of unreserved URL characters, plus "*" for caddy's wildcard.
// Narrow for the same reason validSiteHost is: every path the ingress is
// programmed with is a literal route prefix Town OS composed itself, so
// anything outside this set is a bug upstream rather than a shape somebody
// needs. Whitespace is refused because caddy reads a space-separated matcher
// list as several matchers, which would claim paths nobody asked for.
func validPathMatcher(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") || len(p) > maxPathMatcherLen {
		return false
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/', r == '-', r == '_', r == '.', r == '*', r == '~':
		default:
			return false
		}
	}
	return true
}

// maxPathMatcherLen bounds a path matcher. Nothing in caddy needs the limit;
// it is here so a pathological value cannot be rendered into the config at all.
const maxPathMatcherLen = 255

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
