// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/caddysup"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// renderCaddyfile writes a route's hostname into the Caddyfile with no
// validation and no escaping:
//
//	fmt.Fprintf(&b, "\nhttps://%s {\n", siteAddr(host, httpsPort, 443))
//
// Hostnames reach it from package FQDNs and from page domains, and a page's
// domain is an arbitrary string -- SQLitePagesManager validates only that it is
// non-empty (see systemcontroller_pages_domain_traversal_test.go). Two
// consequences, in increasing order of how much they matter:
//
//  1. A hostname carrying "{" / "}" / a newline closes the vhost block early and
//     everything after it is parsed as top-level Caddy configuration.
//
//  2. Whatever it injects, caddy validates the config as a whole. One malformed
//     site address makes `caddy reload` reject the ENTIRE file, so the ingress
//     keeps serving its last-good route set and every subsequent route change
//     silently fails to apply -- or, at boot, nothing is served at all. That is
//     a denial of service against every package and page on the box, reached
//     through one page's domain field, and it needs no traversal and no
//     cleverness about Caddyfile syntax.
//
// dedupeIngressRoutes already exists on the systemcontroller side precisely
// because "caddy rejects the whole config over one bad entry" was known to be
// the failure mode for duplicate vhosts. The same reasoning applies to a vhost
// that is not a hostname at all.
//
// These tests assert the SECURE behaviour and fail against the current code.

// capturingSupervisor records the last Caddyfile it was handed without running
// caddy, so a render can be asserted without a binary on the box.
type capturingSupervisor struct {
	last []byte
}

func (c *capturingSupervisor) Start() error { return nil }
func (c *capturingSupervisor) Reload(content []byte) error {
	c.last = append([]byte(nil), content...)
	return nil
}
func (c *capturingSupervisor) Shutdown() error { return nil }

// injectionHostnames are the shapes a page domain can take that a Caddyfile
// site address must never be built from verbatim.
var injectionHostnames = []struct {
	name     string
	hostname string
	// marker is a directive the injected text would introduce if the hostname
	// were pasted in unescaped.
	marker string
}{
	{
		name:     "closes the block and opens a new site",
		hostname: "evil.example.com {\n\treverse_proxy 198.51.100.9:80\n}\n:9999 {\n\trespond \"pwned\"\n",
		marker:   "respond",
	},
	{
		name:     "injects a global option block",
		hostname: "evil.example.com {\n}\n{\n\tadmin 0.0.0.0:2019\n",
		marker:   "admin 0.0.0.0:2019",
	},
	{
		name:     "injects a directive into the enclosing block",
		hostname: "evil.example.com\n\troot * /etc\n\tfile_server",
		marker:   "file_server",
	},
	{
		name:     "whitespace splits one site address into two",
		hostname: "evil.example.com other.example.com",
		marker:   "other.example.com",
	},
}

// TestRenderCaddyfileRejectsInjectedHostnames asserts the renderer never emits
// a directive that came out of a hostname.
func TestRenderCaddyfileRejectsInjectedHostnames(t *testing.T) {
	for _, tc := range injectionHostnames {
		t.Run(tc.name, func(t *testing.T) {
			route := &ingresspb.Route{
				Hostname: tc.hostname,
				Backend:  "town-os-system--pages:80",
				CertDir:  "/c/evil",
			}
			out := string(renderCaddyfile([]*ingresspb.Route{route}, 443, 80, ""))

			if strings.Contains(out, tc.marker) {
				t.Errorf("rendered Caddyfile carries %q, injected through a hostname:\n%s", tc.marker, out)
			}
		})
	}
}

// A malformed hostname must not be able to take the rest of the route set with
// it. The renderer's options are to drop the bad route or to escape it; either
// way the good one has to survive, because the alternative is that one page's
// domain field silently unpublishes every service on the box.
func TestRenderCaddyfileBadHostnameDoesNotDropGoodRoutes(t *testing.T) {
	good := &ingresspb.Route{
		Hostname: "gitea.core.home",
		Backend:  "town-os-package--core-gitea-1.0:3000",
		CertDir:  "/c/gitea",
	}

	for _, tc := range injectionHostnames {
		t.Run(tc.name, func(t *testing.T) {
			bad := &ingresspb.Route{
				Hostname: tc.hostname,
				Backend:  "town-os-system--pages:80",
				CertDir:  "/c/evil",
			}
			out := string(renderCaddyfile([]*ingresspb.Route{good, bad}, 443, 80, ""))

			if !strings.Contains(out, "https://gitea.core.home {") {
				t.Errorf("a malformed hostname removed the healthy route from the config:\n%s", out)
			}
			if strings.Contains(out, tc.marker) {
				t.Errorf("rendered Caddyfile carries %q, injected through a hostname:\n%s", tc.marker, out)
			}
		})
	}
}

// The same assertion one layer up, through the gRPC service, so a fix that
// filters in SetRoutes rather than in the renderer also satisfies it.
func TestIngressSetRoutesRejectsInjectedHostnames(t *testing.T) {
	for _, tc := range injectionHostnames {
		t.Run(tc.name, func(t *testing.T) {
			sup := &capturingSupervisor{}
			srv := NewServer(sup, 443, 80, "")

			_, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{
				Routes: []*ingresspb.Route{
					{Hostname: "gitea.core.home", Backend: "town-os-package--core-gitea-1.0:3000", CertDir: "/c/gitea"},
					{Hostname: tc.hostname, Backend: "town-os-system--pages:80", CertDir: "/c/evil"},
				},
			})
			if err != nil {
				// Refusing the request outright is a legitimate fix.
				return
			}

			out := string(sup.last)
			if strings.Contains(out, tc.marker) {
				t.Errorf("SetRoutes rendered %q, injected through a hostname:\n%s", tc.marker, out)
			}
			if !strings.Contains(out, "https://gitea.core.home {") {
				t.Errorf("a malformed hostname removed the healthy route from the config:\n%s", out)
			}
		})
	}
}

// TestIngressBadHostnameDoesNotBreakCaddyValidation is the end-to-end half: it
// asks the real caddy binary whether the rendered config is loadable.
//
// This is the assertion that matters operationally. `caddy validate` failing is
// exactly what `caddy reload` does in production, and a reload that fails means
// the ingress keeps serving whatever it had and every later route change is
// dropped on the floor.
func TestIngressBadHostnameDoesNotBreakCaddyValidation(t *testing.T) {
	caddyBin := findCaddy(t)

	good := &ingresspb.Route{
		Hostname: "gitea.core.home",
		Backend:  "town-os-package--core-gitea-1.0:3000",
		CertDir:  "/c/gitea",
	}

	for _, tc := range injectionHostnames {
		t.Run(tc.name, func(t *testing.T) {
			bad := &ingresspb.Route{
				Hostname: tc.hostname,
				Backend:  "town-os-system--pages:80",
				CertDir:  "/c/evil",
			}
			content := renderCaddyfile([]*ingresspb.Route{good, bad}, 443, 80, "")

			path := filepath.Join(t.TempDir(), "Caddyfile")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatalf("write Caddyfile: %v", err)
			}

			// `caddy validate` parses and provisions the config without binding
			// ports, which is the same parse `caddy reload` performs.
			out, err := exec.CommandContext(context.Background(), caddyBin, "validate", "--config", path, "--adapter", "caddyfile").CombinedOutput() //nolint:gosec // G204 -- binary resolved by findCaddy, path is the test's temp dir
			if err != nil {
				t.Errorf("caddy rejected the whole config because of one malformed hostname: %v\n%s\n--- config ---\n%s",
					err, string(out), string(content))
			}
		})
	}
}

// TestRenderCaddyfileDropsUnderscoreHostnames pins that a site address is a
// hostname, not merely "something without Caddyfile syntax".
//
// An underscore restructures nothing, so a check aimed only at injection lets
// it through — and the route then renders a vhost the local CA cannot issue a
// leaf for, because an underscore is not legal in a certificate's dNSName SAN.
// The result is a service that resolves and then fails every handshake, which
// is harder to diagnose than one that was never published. The good route
// beside it must still survive, for the same reason as everywhere else here.
func TestRenderCaddyfileDropsUnderscoreHostnames(t *testing.T) {
	good := &ingresspb.Route{
		Hostname: "gitea.core.home",
		Backend:  "town-os-package--core-gitea-1.0:3000",
		CertDir:  "/c/gitea",
	}

	for _, host := range []string{
		"my_site.home",
		"site.my_zone.home",
		"_acme-challenge.example.com",
	} {
		t.Run(host, func(t *testing.T) {
			bad := &ingresspb.Route{Hostname: host, Backend: "town-os-system--pages:80", CertDir: "/c/bad"}
			out := string(renderCaddyfile([]*ingresspb.Route{good, bad}, 443, 80, ""))

			if strings.Contains(out, host) {
				t.Errorf("rendered a vhost for %q; an underscore is not a hostname and cannot be covered by a leaf:\n%s", host, out)
			}
			if !strings.Contains(out, "https://gitea.core.home {") {
				t.Errorf("dropping the underscore route removed the healthy route too:\n%s", out)
			}
		})
	}
}

// The dash is the character an underscore is usually mistaken for. It is legal
// inside a label and must keep working, so the rule above cannot be "reject
// anything unusual".
func TestRenderCaddyfileKeepsDashedHostnames(t *testing.T) {
	for _, host := range []string{
		"my-site.home",
		"town-os-package--core-gitea-1.0.core.home",
		"s3.gfeh.office",
	} {
		t.Run(host, func(t *testing.T) {
			route := &ingresspb.Route{Hostname: host, Backend: "town-os-system--pages:80", CertDir: "/c/ok"}
			out := string(renderCaddyfile([]*ingresspb.Route{route}, 443, 80, ""))
			if !strings.Contains(out, "https://"+host+" {") {
				t.Errorf("dropped a legitimate hostname %q:\n%s", host, out)
			}
		})
	}
}

// Guard against a fix that simply drops everything: an ordinary route set must
// still render and still validate.
func TestIngressWellFormedHostnamesStillRender(t *testing.T) {
	sup := &capturingSupervisor{}
	srv := NewServer(sup, 443, 80, "town-os-system--ui:80")

	if _, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{
		Routes: []*ingresspb.Route{
			{Hostname: "gitea.core.home", Backend: "town-os-package--core-gitea-1.0:3000", CertDir: "/c/gitea"},
			{Hostname: "blog.example.com", Backend: "town-os-system--pages:80", CertDir: "/c/blog", ServeHttp: true},
			{Hostname: "s3.gfeh.office", Backend: "town-os-system--gfeh-office:9000", CertDir: "/c/s3"},
		},
	}); err != nil {
		t.Fatalf("SetRoutes with well-formed hostnames: %v", err)
	}

	out := string(sup.last)
	for _, want := range []string{
		"https://gitea.core.home {",
		"https://blog.example.com {",
		"http://blog.example.com {",
		"https://s3.gfeh.office {",
		"reverse_proxy town-os-system--ui:80",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config is missing %q:\n%s", want, out)
		}
	}
}

// caddysup.CaddySupervisor is satisfied by capturingSupervisor; asserted at
// compile time so a change to the interface fails here rather than confusingly
// at the NewServer call.
var _ caddysup.CaddySupervisor = (*capturingSupervisor)(nil)
