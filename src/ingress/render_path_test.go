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
	out := string(renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, ""))

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
	rooted := strings.Index(out, "handle / {")
	catchAll := strings.Index(out, "handle {")
	if rooted < 0 || catchAll < 0 || catchAll < rooted {
		t.Errorf("the catch-all handle is not last (root at %d, catch-all at %d):\n%s", rooted, catchAll, out)
	}
}

// Only the root. A prefix would shadow paths gfehd may grow later, which is the
// failure the index exists to fix, inverted.
func TestRenderCaddyfilePathBackendMatchesTheRootOnly(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, ""))

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
	}}, 443, 80, ""))

	if !strings.Contains(out, "\treverse_proxy town-os-package--core-gitea-1.0:3000\n") {
		t.Errorf("a plain route no longer renders a plain reverse_proxy:\n%s", out)
	}
	if strings.Contains(out, "handle") {
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
			out := string(renderCaddyfile([]*ingresspb.Route{r}, 443, 80, ""))

			if strings.Contains(out, "evil") {
				t.Errorf("an injected path reached the config:\n%s", out)
			}
			if strings.Contains(out, "handle") {
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
	out := string(renderCaddyfile([]*ingresspb.Route{r}, 443, 80, ""))

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
	out := string(renderCaddyfile([]*ingresspb.Route{r}, 443, 80, ""))

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
	first := renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, "")
	for i := range 5 {
		if got := renderCaddyfile([]*ingresspb.Route{httpViewRoute()}, 443, 80, ""); string(got) != string(first) {
			t.Fatalf("render %d differs:\n%s\n---\n%s", i, string(first), string(got))
		}
	}
}

// The syntax has to be caddy's, not something that merely looks right. This is
// the same parse `caddy reload` performs, and a config it rejects takes every
// vhost on the box down rather than the one that is wrong.
func TestRenderCaddyfilePathBackendValidatesWithCaddy(t *testing.T) {
	caddyBin := findCaddy(t)

	content := renderCaddyfile([]*ingresspb.Route{
		httpViewRoute(),
		{Hostname: "gitea.core.home", Backend: "town-os-package--core-gitea-1.0:3000", CertDir: "/c/gitea"},
	}, 443, 80, "town-os-system--ui:80")

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
