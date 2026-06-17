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
		name      string
		routes    []*ingresspb.Route
		httpsPort int
		want      []string // substrings that must all be present
		notWant   []string // substrings that must be absent
	}{
		{
			name:      "empty routes render only the global block",
			routes:    nil,
			httpsPort: 443,
			want:      []string{"auto_https off", "admin off", "protocols h1 h2"},
			notWant:   []string{"https://"},
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
			out := string(renderCaddyfile(tc.routes, tc.httpsPort))
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

func TestRenderCaddyfileDeterministicOrder(t *testing.T) {
	routes := []*ingresspb.Route{
		{Hostname: "zebra.asdf.home", Backend: "b:1", CertDir: "/c/z"},
		{Hostname: "alpha.asdf.home", Backend: "b:2", CertDir: "/c/a"},
	}
	out := string(renderCaddyfile(routes, 443))
	if a, z := strings.Index(out, "alpha.asdf.home"), strings.Index(out, "zebra.asdf.home"); a < 0 || z < 0 || a > z {
		t.Fatalf("expected alpha before zebra (sorted), got:\n%s", out)
	}
	// Rendering the same routes in a different input order yields identical bytes.
	reordered := []*ingresspb.Route{routes[1], routes[0]}
	if string(renderCaddyfile(reordered, 443)) != out {
		t.Fatal("render is not order-independent")
	}
}
