// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"fmt"
	"sort"
	"strings"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// renderCaddyfile renders the shared :443 ingress Caddyfile for the given
// routes: one `https://<hostname>` vhost per route, terminating TLS with the
// route's file-pinned local-CA leaf (or an explicit ACME issuer for a public
// FQDN) and reverse-proxying to the backend container on the ingress network.
//
// Output is sorted by hostname so the bytes are deterministic across reconciles
// — that is what lets the caddy supervisor no-op a Reload whose content has not
// changed. A route with no issued leaf yet (non-ACME, empty cert dir) is skipped
// so a half-provisioned entry never makes caddy reject the whole config. Globals
// mirror the legacy ingress: auto_https off (we manage certs), admin off, and h1
// h2 only (the ingress publishes TCP only, so H3/QUIC over UDP is unreachable).
//
// httpsPort is the TCP port the vhosts bind. Production uses 443 (rendered as a
// bare `https://host`); tests pass an ephemeral port (rendered as
// `https://host:PORT`) so make test-full never collides on a privileged port.
func renderCaddyfile(routes []*ingresspb.Route, httpsPort int) []byte {
	sorted := append([]*ingresspb.Route(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GetHostname() < sorted[j].GetHostname()
	})

	var b strings.Builder
	b.WriteString("{\n\tauto_https off\n\tadmin off\n\tservers {\n\t\tprotocols h1 h2\n\t}\n}\n")
	for _, r := range sorted {
		host := r.GetHostname()
		if host == "" || r.GetBackend() == "" {
			continue
		}
		if !r.GetAcme() && r.GetCertDir() == "" {
			continue
		}
		fmt.Fprintf(&b, "\nhttps://%s {\n", siteAddr(host, httpsPort))
		if r.GetAcme() {
			b.WriteString("\ttls {\n\t\tissuer acme\n\t}\n")
		} else {
			cd := r.GetCertDir()
			fmt.Fprintf(&b, "\ttls %s/cert.pem %s/key.pem\n", cd, cd)
		}
		if r.GetBackendTls() {
			// Backend serves HTTPS (e.g. a self-signed admin UI): proxy over
			// https and skip verification on the internal hop — the browser
			// still validates the ingress's trusted leaf.
			fmt.Fprintf(&b, "\treverse_proxy https://%s {\n\t\ttransport http {\n\t\t\ttls_insecure_skip_verify\n\t\t}\n\t}\n", r.GetBackend())
		} else {
			fmt.Fprintf(&b, "\treverse_proxy %s\n", r.GetBackend())
		}
		b.WriteString("}\n")
	}
	return []byte(b.String())
}

// siteAddr returns the Caddy site address for a host on the given port. Port
// 443 (or 0) renders as a bare host so Caddy uses its HTTPS default; any other
// port is appended explicitly for test isolation.
func siteAddr(host string, httpsPort int) string {
	if httpsPort == 0 || httpsPort == 443 {
		return host
	}
	return fmt.Sprintf("%s:%d", host, httpsPort)
}
