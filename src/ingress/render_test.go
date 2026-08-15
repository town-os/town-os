// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"strings"
	"testing"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

func TestRenderCaddyfile(t *testing.T) {
	tests := []struct {
		name           string
		routes         []*ingresspb.Route
		httpsPort      int
		httpPort       int
		defaultBackend string
		want           []string // substrings that must all be present
		notWant        []string // substrings that must be absent
	}{
		{
			name:      "empty routes render only the global block",
			routes:    nil,
			httpsPort: 443,
			want:      []string{"auto_https off", "protocols h1 h2", "admin 127.0.0.1:2019"},
			// admin must stay enabled so the supervisor's `caddy reload` can push
			// route updates; `admin off` would break it.
			notWant: []string{"https://", "admin off", ":80 {"},
		},
		{
			name: "page route serves over plain HTTP and HTTPS",
			routes: []*ingresspb.Route{{
				Hostname:  "blog.asdf.home",
				Backend:   "town-os-system--pages:80",
				CertDir:   "/etc/town-os/tls/leaves/pages/blog/current",
				ServeHttp: true,
			}},
			httpsPort: 443,
			want: []string{
				"https://blog.asdf.home {",
				"http://blog.asdf.home {",
				"reverse_proxy town-os-system--pages:80",
			},
			// A page is served directly over HTTP, never redirected.
			notWant: []string{"redir "},
		},
		{
			name: "package route redirects HTTP to HTTPS on :80",
			routes: []*ingresspb.Route{{
				Hostname: "gitea.asdf.home",
				Backend:  "town-os-package--asdf-gitea-1.0:3000",
				CertDir:  "/etc/town-os/tls/leaves/asdf/gitea/1.0",
			}},
			httpsPort: 443,
			want: []string{
				"https://gitea.asdf.home {",
				"http://gitea.asdf.home {",
				"redir https://gitea.asdf.home{uri} permanent",
			},
		},
		{
			name:           "default backend catches unmatched hosts on :80",
			routes:         nil,
			httpsPort:      443,
			defaultBackend: "town-os-system--ui:80",
			want:           []string{":80 {", "reverse_proxy town-os-system--ui:80"},
		},
		{
			name: "ephemeral http port is appended to the http site address",
			routes: []*ingresspb.Route{{
				Hostname:  "blog.asdf.home",
				Backend:   "b:80",
				CertDir:   "/c/blog",
				ServeHttp: true,
			}},
			httpsPort:      8443,
			httpPort:       8080,
			defaultBackend: "ui:80",
			want:           []string{"http://blog.asdf.home:8080 {", ":8080 {"},
		},
		{
			name: "file-cert package route",
			routes: []*ingresspb.Route{{
				Hostname: "gitea.asdf.home",
				Backend:  "town-os-package--asdf-gitea-1.0:3000",
				CertDir:  "/etc/town-os/tls/leaves/asdf/gitea/1.0",
			}},
			httpsPort: 443,
			want: []string{
				"https://gitea.asdf.home {",
				"tls /etc/town-os/tls/leaves/asdf/gitea/1.0/cert.pem /etc/town-os/tls/leaves/asdf/gitea/1.0/key.pem",
				"reverse_proxy town-os-package--asdf-gitea-1.0:3000",
			},
			notWant: []string{"issuer acme", "tls_insecure_skip_verify"},
		},
		{
			name: "acme public route ignores cert dir",
			routes: []*ingresspb.Route{{
				Hostname: "git.example.com",
				Backend:  "town-os-package--asdf-gitea-1.0:3000",
				Acme:     true,
			}},
			httpsPort: 443,
			want:      []string{"https://git.example.com {", "issuer acme", "reverse_proxy town-os-package--asdf-gitea-1.0:3000"},
			notWant:   []string{"cert.pem"},
		},
		{
			name: "backend_tls proxies over https with insecure skip verify",
			routes: []*ingresspb.Route{{
				Hostname:   "admin.asdf.home",
				Backend:    "town-os-package--asdf-admin-1.0:8443",
				CertDir:    "/etc/town-os/tls/leaves/asdf/admin/1.0",
				BackendTls: true,
			}},
			httpsPort: 443,
			want:      []string{"reverse_proxy https://town-os-package--asdf-admin-1.0:8443", "tls_insecure_skip_verify"},
		},
		{
			name: "half-provisioned route (no cert, not acme) is skipped",
			routes: []*ingresspb.Route{{
				Hostname: "pending.asdf.home",
				Backend:  "town-os-package--asdf-pending-1.0:80",
			}},
			httpsPort: 443,
			notWant:   []string{"pending.asdf.home"},
		},
		{
			name: "non-443 port is appended to the site address",
			routes: []*ingresspb.Route{{
				Hostname: "gitea.asdf.home",
				Backend:  "backend:3000",
				CertDir:  "/certs/gitea",
			}},
			httpsPort: 8443,
			want:      []string{"https://gitea.asdf.home:8443 {"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := string(renderCaddyfile(tc.routes, tc.httpsPort, tc.httpPort, tc.defaultBackend, false))
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("rendered config missing %q\n--- config ---\n%s", w, out)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(out, nw) {
					t.Errorf("rendered config unexpectedly contains %q\n--- config ---\n%s", nw, out)
				}
			}
		})
	}
}

// TestRenderCaddyfileHTTPBlocks pins the exact :80 behavior per route kind by
// counting directives, so a package can never silently start serving content
// over plain HTTP and a page can never be turned into a redirect.
func TestRenderCaddyfileHTTPBlocks(t *testing.T) {
	page := &ingresspb.Route{
		Hostname: "blog.asdf.home", Backend: "town-os-system--pages:80",
		CertDir: "/c/blog", ServeHttp: true,
	}
	pkg := &ingresspb.Route{
		Hostname: "gitea.asdf.home", Backend: "town-os-package--asdf-gitea-1.0:3000",
		CertDir: "/c/gitea",
	}
	unprovisioned := &ingresspb.Route{
		Hostname: "pending.asdf.home", Backend: "town-os-package--asdf-pending-1.0:80",
	}

	// A page proxies on BOTH :443 and :80 (two reverse_proxy), and never redirects.
	pageOut := string(renderCaddyfile([]*ingresspb.Route{page}, 443, 80, "", false))
	if n := strings.Count(pageOut, "reverse_proxy"); n != 2 {
		t.Errorf("page: want 2 reverse_proxy (https + http), got %d\n%s", n, pageOut)
	}
	if strings.Contains(pageOut, "redir ") {
		t.Errorf("page must never redirect on :80\n%s", pageOut)
	}

	// A package proxies ONLY on :443 (one reverse_proxy) and redirects on :80 —
	// it must not serve content over plain HTTP.
	pkgOut := string(renderCaddyfile([]*ingresspb.Route{pkg}, 443, 80, "", false))
	if n := strings.Count(pkgOut, "reverse_proxy"); n != 1 {
		t.Errorf("package: want exactly 1 reverse_proxy (https only), got %d\n%s", n, pkgOut)
	}
	if n := strings.Count(pkgOut, "redir https://gitea.asdf.home{uri} permanent"); n != 1 {
		t.Errorf("package: want exactly 1 :80->:443 redirect, got %d\n%s", n, pkgOut)
	}

	// A half-provisioned, non-page route (no cert, no acme, not ServeHttp) emits
	// NO vhost at all: neither an HTTPS block (no cert) nor an HTTP one.
	unOut := string(renderCaddyfile([]*ingresspb.Route{unprovisioned}, 443, 80, "", false))
	if strings.Contains(unOut, "pending.asdf.home") {
		t.Errorf("unprovisioned route must emit no vhost\n%s", unOut)
	}
	if strings.Count(unOut, "reverse_proxy") != 0 || strings.Contains(unOut, "redir ") {
		t.Errorf("unprovisioned route must emit no proxy/redirect\n%s", unOut)
	}

	// Without a default backend there is no bare :80 catch-all; with one there is
	// exactly one extra reverse_proxy to the UI.
	if strings.Contains(unOut, ":80 {") {
		t.Errorf("no default backend must not emit a :80 catch-all\n%s", unOut)
	}
	defOut := string(renderCaddyfile(nil, 443, 80, "town-os-system--ui:80", false))
	if !strings.Contains(defOut, ":80 {") || strings.Count(defOut, "reverse_proxy town-os-system--ui:80") != 1 {
		t.Errorf("default backend must emit one :80 catch-all to the UI\n%s", defOut)
	}
}

func TestRenderCaddyfileDeterministicOrder(t *testing.T) {
	routes := []*ingresspb.Route{
		{Hostname: "zebra.asdf.home", Backend: "b:1", CertDir: "/c/z"},
		{Hostname: "alpha.asdf.home", Backend: "b:2", CertDir: "/c/a"},
	}
	out := string(renderCaddyfile(routes, 443, 80, "", false))
	if a, z := strings.Index(out, "alpha.asdf.home"), strings.Index(out, "zebra.asdf.home"); a < 0 || z < 0 || a > z {
		t.Fatalf("expected alpha before zebra (sorted), got:\n%s", out)
	}
	// Rendering the same routes in a different input order yields identical bytes.
	reordered := []*ingresspb.Route{routes[1], routes[0]}
	if string(renderCaddyfile(reordered, 443, 80, "", false)) != out {
		t.Fatal("render is not order-independent")
	}
}

// TestRenderCaddyfileEmitsTheAdminEndpoint pins the global option that decides
// which caddy a `caddy reload` reaches.
//
// The port has to appear in the file rather than being left to caddy's default,
// because `caddy reload` reads the admin address out of the config it adapts —
// so the rendered address IS the address the supervisor talks to. A render that
// dropped this line would put every test's caddy back on the shared 2019, and
// the way that fails is not a clean bind error: the first run's reload would
// find the second run's admin API and program it with the first run's routes.
func TestRenderCaddyfileEmitsTheAdminEndpoint(t *testing.T) {
	const relocated = 41919

	out, _ := renderCaddyfileTally(nil, 443, 80, relocated, "", false)
	if got, want := string(out), "\tadmin 127.0.0.1:41919\n"; !strings.Contains(got, want) {
		t.Errorf("rendered Caddyfile does not carry %q:\n%s", want, got)
	}
	if strings.Contains(string(out), ":2019") {
		t.Errorf("rendered Caddyfile still names the default admin port:\n%s", out)
	}

	// Zero means the default, matching every other port in this renderer.
	zero, _ := renderCaddyfileTally(nil, 443, 80, 0, "", false)
	if got, want := string(zero), "\tadmin 127.0.0.1:2019\n"; !strings.Contains(got, want) {
		t.Errorf("admin port 0 should render the default %q:\n%s", want, got)
	}
}

// The admin address is loopback in every rendering. Caddy's admin API can
// replace the entire running config, so one reachable from the LAN would let
// anyone who can reach the box re-point every hostname it serves.
func TestRenderCaddyfileAdminStaysOnLoopback(t *testing.T) {
	for _, port := range []int{0, DefaultAdminPort, 41919} {
		out, _ := renderCaddyfileTally(nil, 443, 80, port, "", false)
		for _, bad := range []string{"admin 0.0.0.0:", "admin :", "admin [::]:"} {
			if strings.Contains(string(out), bad) {
				t.Errorf("admin port %d rendered a non-loopback endpoint (%q):\n%s", port, bad, out)
			}
		}
	}
}
