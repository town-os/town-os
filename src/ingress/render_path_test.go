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

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// httpViewRoute is the one shape in the product that uses a path backend: the
// gfeh http view, whose root serves the published-files index out of the pages
// container while every other path stays gfehd's.
func httpViewRoute() *ingresspb.Route {
	return &ingresspb.Route{
		Hostname: "http.gfeh.home",
		Backend:  "town-os-system--gfeh-home:9001",
		CertDir:  "/etc/town-os/tls/leaves/gfeh/home/current",
		PathBackends: []*ingresspb.PathBackend{{
			Path:    "/",
			Backend: "town-os-system--pages:80",
		}},
	}
}

// A path backend splits one vhost between two services: the matched path to the
// path backend, everything else to the route's own.
func TestRenderCaddyfilePathBackendSplitsTheVhost(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, "", false))

	for _, want := range []string{
		"https://http.gfeh.home {",
		"handle / {",
		"reverse_proxy town-os-system--pages:80",
		"handle {",
		"reverse_proxy town-os-system--gfeh-home:9001",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config is missing %q:\n%s", want, out)
		}
	}

	// The catch-all has to come last. handle blocks are evaluated in order and
	// a bare one matches everything, so a bare block written first would make
	// every path backend after it dead config — and the index would never be
	// reached even though it renders in the file.
	sites := sitesOnly(out)
	rooted := strings.Index(sites, "handle / {")
	catchAll := strings.Index(sites, "handle {")
	if rooted < 0 || catchAll < 0 || catchAll < rooted {
		t.Errorf("the catch-all handle is not last (root at %d, catch-all at %d):\n%s", rooted, catchAll, out)
	}
}

// Only the root. A prefix would shadow paths gfehd may grow later, which is the
// failure the index exists to fix, inverted.
func TestRenderCaddyfilePathBackendMatchesTheRootOnly(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, "", false))

	if strings.Contains(out, "handle /* {") || strings.Contains(out, "handle /f/") {
		t.Errorf("the path backend claims more than the root:\n%s", out)
	}
}

// The overwhelmingly common route has no path backends at all, and must render
// exactly as it did before the field existed: one reverse_proxy, no handle.
func TestRenderCaddyfileWithoutPathBackendsIsUnchanged(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{{
		Hostname: "gitea.core.home",
		Backend:  "town-os-package--core-gitea-1.0:3000",
		CertDir:  "/c/gitea",
	}}, 443, 80, "", false))

	if !strings.Contains(out, "\treverse_proxy town-os-package--core-gitea-1.0:3000 {\n") {
		t.Errorf("a plain route no longer renders a plain reverse_proxy:\n%s", out)
	}
	// sitesOnly, because the retry-page snippets carry handle blocks of their
	// own and every site block imports them. What this test is about is that the
	// VHOST has no path-splitting handles, not that the word never appears.
	if strings.Contains(sitesOnly(out), "handle") {
		t.Errorf("a route with no path backends rendered handle blocks:\n%s", out)
	}
}

// A path is pasted into a handle directive, so it gets the same treatment a
// hostname and a backend get: one malformed value must cost that value, not the
// route it is on and not every other vhost on the box.
func TestRenderCaddyfileDropsInjectedPathBackends(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{"closes the block", "/ }\nhttps://evil.home {\n\treverse_proxy evil:80"},
		{"a second matcher", "/ /etc"},
		{"an opening brace", "/{"},
		{"a newline", "/\n"},
		{"not a path at all", "index"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httpViewRoute()
			r.PathBackends[0].Path = tc.path
			out := string(renderCaddyfile([]*ingresspb.Route{r}, 443, 80, "", false))

			if strings.Contains(out, "evil") {
				t.Errorf("an injected path reached the config:\n%s", out)
			}
			if strings.Contains(sitesOnly(out), "handle") {
				t.Errorf("a rejected path backend still rendered a handle block:\n%s", out)
			}
			// The route survives on its own backend: losing the index is not a
			// reason to stop serving published links.
			if !strings.Contains(out, "reverse_proxy town-os-system--gfeh-home:9001") {
				t.Errorf("the route lost its own backend over a bad path backend:\n%s", out)
			}
		})
	}
}

// A backend is host:port wherever it appears, including on a path backend.
func TestRenderCaddyfileDropsInjectedPathBackendTargets(t *testing.T) {
	r := httpViewRoute()
	r.PathBackends[0].Backend = "pages:80 }\nhttps://evil.home {\n\treverse_proxy evil:80"
	out := string(renderCaddyfile([]*ingresspb.Route{r}, 443, 80, "", false))

	if strings.Contains(out, "evil") {
		t.Errorf("an injected path backend target reached the config:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy town-os-system--gfeh-home:9001") {
		t.Errorf("the route lost its own backend:\n%s", out)
	}
}

// Two handle blocks with the same matcher are accepted by caddy and the second
// is simply unreachable, so the duplicate is dropped where it can still be
// reported rather than silently ignored at request time.
func TestRenderCaddyfileDropsDuplicatePathBackends(t *testing.T) {
	r := httpViewRoute()
	r.PathBackends = append(r.PathBackends, &ingresspb.PathBackend{
		Path:    "/",
		Backend: "town-os-system--other:80",
	})
	out := string(renderCaddyfile([]*ingresspb.Route{r}, 443, 80, "", false))

	if n := strings.Count(out, "handle / {"); n != 1 {
		t.Errorf("got %d handle blocks for /, want 1:\n%s", n, out)
	}
	// First wins, matching dedupeIngressRoutes.
	if !strings.Contains(out, "reverse_proxy town-os-system--pages:80") {
		t.Errorf("the first path backend did not win:\n%s", out)
	}
	if strings.Contains(out, "town-os-system--other:80") {
		t.Errorf("the duplicate path backend rendered:\n%s", out)
	}
}

// The rendered bytes are compared against the running config to decide whether
// to reload, so a route that rendered differently between identical reconciles
// would bounce caddy every pass.
func TestRenderCaddyfilePathBackendsAreDeterministic(t *testing.T) {
	first := renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, "", false)
	for i := range 5 {
		if got := renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, "", false); string(got) != string(first) {
			t.Fatalf("render %d differs:\n%s\n---\n%s", i, string(first), string(got))
		}
	}
}

// The syntax has to be caddy's, not something that merely looks right. This is
// the same parse `caddy reload` performs, and a config it rejects takes every
// vhost on the box down rather than the one that is wrong.
func TestRenderCaddyfilePathBackendValidatesWithCaddy(t *testing.T) {
	caddyBin := findCaddy(t)

	// Real leaves: validate provisions the TLS app and opens every certificate
	// the config names, so a made-up CertDir fails as though the config were
	// malformed (see testLeafDir).
	gfeh := httpViewRoute()
	gfeh.CertDir = testLeafDir(t, gfeh.GetHostname())
	content := renderCaddyfile([]*ingresspb.Route{
		gfeh,
		{
			Hostname: "gitea.core.home", Backend: "town-os-package--core-gitea-1.0:3000",
			CertDir: testLeafDir(t, "gitea.core.home"),
		},
	}, 443, 80, "town-os-system--ui:80", false)

	path := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	out, err := exec.CommandContext(context.Background(), caddyBin,
		"validate", "--config", path, "--adapter", "caddyfile").CombinedOutput()
	if err != nil {
		t.Errorf("caddy rejected a config with a path backend: %v\n%s\n--- config ---\n%s",
			err, string(out), string(content))
	}
}

func TestValidPathMatcher(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/f/*", true},
		{"/.well-known/acme-challenge/*", true},
		{"/a-b_c.d~e", true},
		{"", false},
		{"f", false},          // must be absolute
		{"/a b", false},       // caddy reads a space as a second matcher
		{"/a\nb", false},      // restructures the file
		{"/a{b", false},       // opens a block
		{"/a}b", false},       // closes one
		{"/a\"b", false},      // quoting
		{"/" + strings.Repeat("a", maxPathMatcherLen), false},
	} {
		if got := validPathMatcher(tc.path); got != tc.want {
			t.Errorf("validPathMatcher(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// A path backend that terminates its own TLS is proxied over https with
// verification skipped on the internal hop — the same treatment a route-level
// TLS backend gets.
//
// This is what lets one vhost front a DoH resolver at /dns-query while serving
// something else at every other path: rolodex's DoH listener speaks TLS, and the
// scheme used to be hardcoded to plain HTTP for every path backend. That failure
// is a 502 with nothing in it to say the config chose the wrong scheme.
func TestRenderCaddyfilePathBackendKeepsItsOwnScheme(t *testing.T) {
	route := &ingresspb.Route{
		Hostname: "dns.home",
		Backend:  "town-os-system--ui:80",
		CertDir:  "/etc/town-os/tls/leaves/dns/home/current",
		PathBackends: []*ingresspb.PathBackend{{
			Path:       "/dns-query",
			Backend:    "127.0.0.2:443",
			BackendTls: true,
		}},
	}

	out := string(renderCaddyfile([]*ingresspb.Route{route}, 443, 80, "", false))

	for _, want := range []string{
		"handle /dns-query {",
		"reverse_proxy https://127.0.0.2:443 {",
		"tls_insecure_skip_verify",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config is missing %q:\n%s", want, out)
		}
	}
	// The path is handled, not stripped: rolodex serves /dns-query and nothing
	// else, so a handle_path here would proxy "/" and every query would 404.
	if strings.Contains(out, "handle_path") {
		t.Errorf("the DoH path is stripped before it reaches rolodex:\n%s", out)
	}
	// The route's own backend is untouched by the path backend's scheme.
	if !strings.Contains(out, "reverse_proxy town-os-system--ui:80") {
		t.Errorf("the route's own backend lost its scheme:\n%s", out)
	}
}

// The default stays plain HTTP: an unset flag must not start proxying https to
// the pages container, which speaks :80.
func TestRenderCaddyfilePathBackendDefaultsToPlainHTTP(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, "", false))

	if strings.Contains(out, "reverse_proxy https://town-os-system--pages:80") {
		t.Errorf("a path backend with no TLS flag was proxied over https:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy town-os-system--pages:80") {
		t.Errorf("rendered config is missing the plain-HTTP pages backend:\n%s", out)
	}
}

// The :80 fallback backend can speak HTTPS too. It was the last hop in the
// renderer that could only be plaintext, which meant a fallback service holding
// its own certificate could not be fronted at all — the proxy would send
// plaintext at a TLS socket and every unmatched host would 502.
func TestRenderCaddyfileDefaultBackendCanSpeakHTTPS(t *testing.T) {
	out := string(renderCaddyfile(nil, 443, 80, "town-os-system--ui:443", true))

	for _, want := range []string{
		":80 {",
		"reverse_proxy https://town-os-system--ui:443 {",
		"tls_insecure_skip_verify",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config is missing %q:\n%s", want, out)
		}
	}
}

// And still defaults to plaintext, because the UI container serves :80.
func TestRenderCaddyfileDefaultBackendStaysPlainByDefault(t *testing.T) {
	out := string(renderCaddyfile(nil, 443, 80, "town-os-system--ui:80", false))

	if strings.Contains(out, "reverse_proxy https://town-os-system--ui:80") {
		t.Errorf("the default backend was proxied over https without being asked:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy town-os-system--ui:80") {
		t.Errorf("rendered config is missing the plain default backend:\n%s", out)
	}
}
