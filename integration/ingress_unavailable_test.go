// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// The retry page, driven against a REAL caddy — which is the only way to test
// it. The rendered Caddyfile carries the page as snippets caddy expands on
// import (handle_response inside every reverse_proxy, handle_errors on every
// site block), so a unit test on the bytes can prove the file SAYS the right
// thing and cannot prove caddy agrees. Everything here is the second half: what
// a client actually receives when a backend answers 5xx, when it does not
// answer at all, and when it comes back.
//
// Same skip as the rest of the caddy-driven suite: the test image carries caddy
// at /usr/bin/caddy, a developer box may not.

package integration_test

import (
	"context"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/caddysup"
	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/ingress"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// browserAccept is what a browser sends on a top-level navigation, and the
// header the retry page is gated on. Anything else — an XHR, a webhook, curl —
// gets the backend's own failure rather than a page it cannot parse.
const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// TestIngressRetryPageOnBackendFiveHundred covers the first of the two ways a
// backend breaks: it answers, with a 5xx. That is an upstream RESPONSE, so it
// is intercepted inside the reverse_proxy with handle_response — a site-level
// error handler never sees it at all.
//
// Both arms matter. A browser gets the retry page; an API client gets the
// backend's own status, body and Content-Type copied through, because handing
// HTML to something parsing JSON turns one broken backend into a second,
// stranger failure in the caller.
func TestIngressRetryPageOnBackendFiveHundred(t *testing.T) {
	caddyBin := findCaddy(t)

	tlsDir, leafDir := issueLeafFor(t, "flaky.local")

	const apiBody = `{"error":"boom"}`
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, apiBody)
	}))
	defer backend.Close()

	port := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	srv := ingress.NewServer(sup, port, freePort(t), "", ingress.WithCaddyAdminPort(freePort(t)))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	if _, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname: "flaky.local",
		Backend:  strings.TrimPrefix(backend.URL, "http://"),
		CertDir:  leafDir,
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := caClient(t, filepath.Join(tlsDir, "ca.crt"), port)

	// The browser arm. Polled because it also gates readiness: the reload is
	// near-instant but not synchronous.
	var status int
	var body string
	if !poll(t, func() bool {
		s, _, b, err := getAccepting(t, client, "https://flaky.local/", browserAccept)
		if err != nil {
			return false
		}
		status, body = s, b
		return strings.Contains(b, "flaky.local is unavailable")
	}) {
		t.Fatalf("a 5xx from the backend did not become the retry page; last response was %d %q",
			status, body)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("retry page status = %d, want %d", status, http.StatusServiceUnavailable)
	}
	assertRetryPage(t, body)

	// The API arm: the backend's own response, untouched.
	apiStatus, apiHeader, gotBody, err := getAccepting(t, client, "https://flaky.local/", "application/json")
	if err != nil {
		t.Fatalf("api GET: %v", err)
	}
	if apiStatus != http.StatusInternalServerError {
		t.Errorf("api client got status %d, want the backend's %d", apiStatus, http.StatusInternalServerError)
	}
	if gotBody != apiBody {
		t.Errorf("api client body = %q, want the backend's %q", gotBody, apiBody)
	}
	if ct := apiHeader.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("api client Content-Type = %q, want the backend's application/json", ct)
	}
	// Each header exactly once. The obvious way to write this passthrough is
	// copy_response_headers followed by copy_response, and it emits every
	// upstream header TWICE — reverse_proxy has already staged them by the time
	// a response handler runs. Two Content-Length fields is a malformed message,
	// not a cosmetic problem, and nothing about the body assertions above would
	// have caught it.
	for _, field := range []string{"Content-Type", "Content-Length"} {
		if n := len(apiHeader.Values(field)); n > 1 {
			t.Errorf("%s arrived %d times, want once: %q", field, n, apiHeader.Values(field))
		}
	}
	if strings.Contains(gotBody, "is unavailable") {
		t.Errorf("api client was served the retry page:\n%s", gotBody)
	}
}

// TestIngressRetryPageWhenTheBackendIsDown covers the other way a backend
// breaks, and the common one on a home box: nothing is listening. A package
// restarting after an upgrade has no socket at all, so reverse_proxy fails and
// caddy raises its OWN error — handle_response is never invoked, and an
// implementation that only intercepted 5xx responses would serve caddy's bare
// 502 here.
//
// Then it starts the backend on the same address and polls the same URL. That
// is the page's actual promise: the address stays routed and starts working
// again with no reload, no reprogramming, and nothing for the user to do.
func TestIngressRetryPageWhenTheBackendIsDown(t *testing.T) {
	caddyBin := findCaddy(t)

	tlsDir, leafDir := issueLeafFor(t, "down.local")

	// An address with nothing behind it yet. Reserved through freePort so two
	// concurrent runs cannot pick the same one — IRON RULE.
	backendPort := freePort(t)
	backendAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(backendPort))

	port := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	srv := ingress.NewServer(sup, port, freePort(t), "", ingress.WithCaddyAdminPort(freePort(t)))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	if _, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname: "down.local",
		Backend:  backendAddr,
		CertDir:  leafDir,
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := caClient(t, filepath.Join(tlsDir, "ca.crt"), port)

	var status int
	var body string
	if !poll(t, func() bool {
		s, _, b, err := getAccepting(t, client, "https://down.local/", browserAccept)
		if err != nil {
			return false
		}
		status, body = s, b
		return strings.Contains(b, "down.local is unavailable")
	}) {
		t.Fatalf("an unreachable backend did not become the retry page; last response was %d %q",
			status, body)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("retry page status = %d, want %d", status, http.StatusServiceUnavailable)
	}
	assertRetryPage(t, body)

	// A non-browser client still gets a retryable answer, not a page.
	apiStatus, apiHeader, apiBody, err := getAccepting(t, client, "https://down.local/", "application/json")
	if err != nil {
		t.Fatalf("api GET: %v", err)
	}
	if apiStatus != http.StatusServiceUnavailable {
		t.Errorf("api client got status %d, want %d", apiStatus, http.StatusServiceUnavailable)
	}
	if apiHeader.Get("Retry-After") == "" {
		t.Errorf("api client got no Retry-After on an unreachable backend")
	}
	if strings.Contains(apiBody, "<html") {
		t.Errorf("api client was served HTML:\n%s", apiBody)
	}

	// The service comes back on the same address. No reload, no SetRoutes.
	const recovered = "BACKEND-IS-BACK"
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", backendAddr)
	if err != nil {
		t.Fatalf("listen on the backend address: %v", err)
	}
	late := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, recovered)
	}))
	if err := late.Listener.Close(); err != nil {
		t.Fatalf("close the placeholder listener: %v", err)
	}
	late.Listener = l
	late.Start()
	defer late.Close()

	if !poll(t, func() bool {
		s, _, b, err := getAccepting(t, client, "https://down.local/", browserAccept)
		if err != nil {
			return false
		}
		status, body = s, b
		return s == http.StatusOK && b == recovered
	}) {
		t.Fatalf("the route did not recover once the backend came back; last response was %d %q",
			status, body)
	}
}

// TestIngressRetryPageOnPlainHTTP covers the two site blocks the HTTPS tests do
// not reach: a page's :80 vhost (pages are served over plain HTTP, never
// redirected) and the bare :80 fallback that catches every unmatched host.
//
// The fallback is the Town OS UI, and it is the one block with no hostname of
// its own — the Host header there is whatever the client sent, so the page names
// it by a constant instead. A page that echoed that header would be an injection
// on the one vhost that accepts any host at all.
func TestIngressRetryPageOnPlainHTTP(t *testing.T) {
	caddyBin := findCaddy(t)

	_, leafDir := issueLeafFor(t, "page.local")

	// Both backends are addresses nothing is listening on: this test is about
	// which vhost serves the page, not about which failure produced it.
	pageAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))
	uiAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))

	httpPort := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	srv := ingress.NewServer(sup, freePort(t), httpPort, uiAddr, ingress.WithCaddyAdminPort(freePort(t)))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	if _, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname:  "page.local",
		Backend:   pageAddr,
		CertDir:   leafDir,
		ServeHttp: true,
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := plainClientNoRedirect(t, httpPort)

	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{name: "page vhost", url: "http://page.local/", want: "page.local is unavailable"},
		{name: "default backend", url: "http://unrouted.invalid/", want: "Town OS is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var status int
			var body string
			if !poll(t, func() bool {
				s, _, b, err := getAccepting(t, client, tc.url, browserAccept)
				if err != nil {
					return false
				}
				status, body = s, b
				return strings.Contains(b, tc.want)
			}) {
				t.Fatalf("%s did not serve the retry page; last response was %d %q", tc.url, status, body)
			}
			if status != http.StatusServiceUnavailable {
				t.Errorf("retry page status = %d, want %d", status, http.StatusServiceUnavailable)
			}
			assertRetryPage(t, body)
		})
	}
}

// TestIngressRetryPageSpeaksTheClientsLanguage drives the language selector
// against real caddy: the page is rendered once per shipped catalog into the
// Caddyfile, and Caddy picks a branch from the client's Accept-Language.
//
// This is the half a unit test cannot reach. The selector is thirty
// Accept-Language matchers and a fallthrough, and whether Caddy's header
// matcher agrees with what the renderer wrote — case-sensitive prefixes, the
// Traditional-before-generic ordering for Chinese, OR across the four zh
// prefixes — is a property of Caddy, not of the bytes.
func TestIngressRetryPageSpeaksTheClientsLanguage(t *testing.T) {
	caddyBin := findCaddy(t)

	tlsDir, leafDir := issueLeafFor(t, "polyglot.local")

	// Nothing listening: the fastest way to a retry page, and this test is
	// about which language it comes back in.
	backendAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))

	port := freePort(t)
	sup := caddysup.NewSupervisor(caddyBin, filepath.Join(t.TempDir(), "Caddyfile"))
	// A configured locale that is NOT English, so the fallthrough arm proves
	// the box's setting is what answers a language Town OS does not ship —
	// against English it would pass either way.
	srv := ingress.NewServer(sup, port, freePort(t), "",
		ingress.WithCaddyAdminPort(freePort(t)), ingress.WithDefaultLocale("de-DE"))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap caddy: %v", err)
	}
	defer func() { _ = sup.Shutdown() }()

	if _, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname: "polyglot.local",
		Backend:  backendAddr,
		CertDir:  leafDir,
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	client := caClient(t, filepath.Join(tlsDir, "ca.crt"), port)

	// Readiness first, so a later language failure is a language failure rather
	// than a route that was not up yet.
	//
	// Asking in English EXPLICITLY, which is the whole point of this box being
	// configured in German: a request with no Accept-Language at all takes the
	// fallthrough and is answered in the configured locale, so a readiness probe
	// that sent no header and waited for English would wait forever — which is
	// exactly how this test failed the first time it ran.
	if !poll(t, func() bool {
		_, _, body, err := getWithHeaders(t, client, "https://polyglot.local/",
			map[string]string{"Accept": browserAccept, "Accept-Language": "en-US,en;q=0.9"})
		return err == nil && strings.Contains(body, "polyglot.local is unavailable")
	}) {
		t.Fatal("the retry page never came up in English for an English client")
	}

	// And the header-less case is the fallthrough, in the box's own language.
	// This is the arm that pins "no preference stated" to the configured locale
	// rather than to en-US.
	_, _, headerless, err := getAccepting(t, client, "https://polyglot.local/", browserAccept)
	if err != nil {
		t.Fatalf("headerless GET: %v", err)
	}
	if want := html.EscapeString(i18n.T("de-DE", i18n.MsgIngressUnavailableBody)); !strings.Contains(headerless, want) {
		t.Errorf("a client that stated no language was not answered in the box's locale:\n%s", headerless)
	}

	// The expected sentence is read out of the catalog rather than written here.
	// Two reasons, and the second is the one that matters: a literal would be a
	// second copy of a translation that can be edited without this test noticing,
	// and every string outside src/i18n is held to Latin script by gosmopolitan —
	// the linter exists precisely so that translations live in one place.
	wantBody := func(code string) string {
		return html.EscapeString(i18n.T(code, i18n.MsgIngressUnavailableBody))
	}

	for _, tc := range []struct {
		name     string
		accept   string
		wantLang string
	}{
		{name: "japanese", accept: "ja,en-US;q=0.9", wantLang: "ja-JP"},
		{name: "german", accept: "de-DE,de;q=0.9", wantLang: "de-DE"},
		{name: "brazilian portuguese", accept: "pt-BR,pt;q=0.9", wantLang: "pt-BR"},
		// Traditional Chinese must not be claimed by the generic zh branch.
		{name: "traditional chinese", accept: "zh-TW,zh;q=0.9", wantLang: "zh-TW"},
		{name: "simplified chinese", accept: "zh-CN,zh;q=0.9", wantLang: "zh-CN"},
		// Arabic is the one right-to-left catalog, and dir is what makes it read.
		{name: "arabic", accept: "ar,en;q=0.8", wantLang: "ar-SA"},
		// A language Town OS ships nothing for falls through to the box's own
		// configured locale — not to English.
		{name: "unshipped language falls back to the box locale", accept: "is-IS,is;q=0.9", wantLang: "de-DE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, body, err := getWithHeaders(t, client, "https://polyglot.local/",
				map[string]string{"Accept": browserAccept, "Accept-Language": tc.accept})
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			if status != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want %d", status, http.StatusServiceUnavailable)
			}
			if want := wantBody(tc.wantLang); !strings.Contains(body, want) {
				t.Errorf("page is not in %s (no %q):\n%s", tc.wantLang, want, body)
			}
			if want := `lang="` + tc.wantLang + `"`; !strings.Contains(body, want) {
				t.Errorf("page does not declare %s:\n%s", want, body)
			}
			// The service name still comes from the config, in whatever place
			// the translated sentence puts it.
			if !strings.Contains(body, "polyglot.local") {
				t.Errorf("the translated page lost the service name:\n%s", body)
			}
		})
	}

	// Right-to-left is marked, and only for the script that needs it.
	_, _, arabic, err := getWithHeaders(t, client, "https://polyglot.local/",
		map[string]string{"Accept": browserAccept, "Accept-Language": "ar"})
	if err != nil {
		t.Fatalf("arabic GET: %v", err)
	}
	if !strings.Contains(arabic, `dir="rtl"`) {
		t.Errorf("the Arabic page is not marked right-to-left:\n%s", arabic)
	}
	_, _, german, err := getWithHeaders(t, client, "https://polyglot.local/",
		map[string]string{"Accept": browserAccept, "Accept-Language": "de"})
	if err != nil {
		t.Fatalf("german GET: %v", err)
	}
	if !strings.Contains(german, `dir="ltr"`) {
		t.Errorf("the German page is not marked left-to-right:\n%s", german)
	}
}

// TestIngressRetryPageConfigValidatesHere asks the real caddy whether the
// PRODUCTION shape of the config is loadable — every route kind at once,
// including the two the serving tests here deliberately cannot use.
//
// It exists because the equivalent unit test cannot run where it lives.
// `TestRenderCaddyfileRetryPageValidatesWithCaddy` is in src/ingress, because
// renderCaddyfile is unexported, and ./src/... runs on the HOST — where a
// developer box has no caddy, so that test reports SKIP and the retry page's
// syntax is checked by nothing. The test image carries caddy at /usr/bin/caddy
// and only ./integration/... is compiled into it, so this is the copy that
// actually runs. Same reasoning as the rest of this suite (see
// ingress_caddy_test.go); the skip is kept there because on a developer box it
// is honest.
//
// The config is captured rather than served, which is what lets it carry an
// ACME route: at load time a real caddy would begin managing that certificate
// and reach for a CA the harness must never depend on. `caddy validate` runs
// the same parse and provisioning `caddy reload` does, without starting cert
// maintenance or binding a port — so nothing here can collide with a concurrent
// make test-full, despite the config naming :443 and :80.
//
// Every file-cert route points at a REAL issued leaf, because validate does not
// stop at parsing: it provisions the TLS app, which opens every certificate the
// config names. A route pointed at a directory that does not exist fails with
// `open /c/admin/cert.pem: no such file or directory` — a failure about the
// fixture rather than about the config under test.
func TestIngressRetryPageConfigValidatesHere(t *testing.T) {
	caddyBin := findCaddy(t)

	leaf := leafIssuer(t)
	sup := &capturingSupervisor{}
	// A non-English fallback, so the default row of the language map carries a
	// catalog whose sentences are not the ones every other row would have.
	srv := ingress.NewServer(sup, 443, 80, "town-os-system--ui:80", ingress.WithDefaultLocale("de-DE"))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := srv.SetRoutes(context.Background(), &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		// A page: served over plain HTTP as well as HTTPS.
		{Hostname: "blog.asdf.home", Backend: "town-os-system--pages:80", CertDir: leaf("blog.asdf.home"), ServeHttp: true},
		// A package: HTTPS only, with a :80 redirect.
		{Hostname: "gitea.asdf.home", Backend: "town-os-package--asdf-gitea-1.0:3000", CertDir: leaf("gitea.asdf.home")},
		// A public FQDN on ACME rather than the local CA.
		{Hostname: "git.example.com", Backend: "town-os-package--asdf-gitea-1.0:3000", Acme: true},
		// A backend that terminates its own TLS.
		{Hostname: "admin.asdf.home", Backend: "town-os-package--asdf-admin-1.0:8443", CertDir: leaf("admin.asdf.home"), BackendTls: true},
		// One vhost split between two backends, the second over TLS — the
		// object-storage index in front of rolodex's DoH listener.
		{
			Hostname: "http.gfeh.home", Backend: "town-os-system--gfeh-home:9001", CertDir: leaf("http.gfeh.home"),
			PathBackends: []*ingresspb.PathBackend{
				{Path: "/", Backend: "town-os-system--pages:80"},
				{Path: "/dns-query", Backend: "127.0.0.2:4443", BackendTls: true},
			},
		},
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	content := sup.last()
	if len(content) == 0 {
		t.Fatal("the supervisor was handed no config at all")
	}
	path := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	out, err := exec.CommandContext(context.Background(), caddyBin,
		"validate", "--config", path, "--adapter", "caddyfile").CombinedOutput()
	if err != nil {
		t.Fatalf("caddy rejected the rendered config: %v\n%s\n--- config ---\n%s",
			err, string(out), string(content))
	}
}

// capturingSupervisor keeps the rendered Caddyfile without running caddy, so a
// config can be validated as a whole rather than served a route at a time.
type capturingSupervisor struct {
	mu      sync.Mutex
	content []byte
}

func (s *capturingSupervisor) Start() error { return nil }

func (s *capturingSupervisor) Reload(content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.content = append([]byte(nil), content...)
	return nil
}

func (s *capturingSupervisor) Shutdown() error { return nil }

func (s *capturingSupervisor) last() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.content
}

// Asserted at compile time so a change to the interface fails here rather than
// confusingly at the NewServer call.
var _ caddysup.CaddySupervisor = (*capturingSupervisor)(nil)

// assertRetryPage checks what the page has to do to be worth serving: say the
// service is unavailable, say it is being retried, and reload itself so the
// saying is true without the reader doing anything.
func assertRetryPage(t *testing.T, body string) {
	t.Helper()
	for _, want := range []string{
		"is unavailable",
		`http-equiv="refresh"`,
		"retries every",
		"not answering",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the retry page is missing %q:\n%s", want, body)
		}
	}
	// The CSS braces are the reason this is asserted from the far end: caddy
	// substitutes placeholders in a respond body, and a `{...}` it decided to
	// recognize would be replaced with nothing, taking the stylesheet with it.
	if !strings.Contains(body, "max-width: 32rem") {
		t.Errorf("the retry page lost its stylesheet in placeholder substitution:\n%s", body)
	}
}

// leafIssuer returns a function that issues a leaf for a hostname from one
// temporary local CA and hands back the directory it lives in.
//
// One CA for the whole route set, rather than issueLeafFor's one CA per call:
// a config validation needs several hosts at once and nothing about it cares
// whether they share an issuer.
func leafIssuer(t *testing.T) func(host string) string {
	t.Helper()
	tlsDir := t.TempDir()
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	return func(host string) string {
		t.Helper()
		dir := filepath.Join(tlsDir, "leaves", host)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir leaf dir for %s: %v", host, err)
		}
		if err := ca.IssueLeaf(dir, []string{host}); err != nil {
			t.Fatalf("IssueLeaf %s: %v", host, err)
		}
		return dir
	}
}

// issueLeafFor returns a temp local-CA directory and a leaf directory for host.
func issueLeafFor(t *testing.T, host string) (tlsDir, leafDir string) {
	t.Helper()
	tlsDir = t.TempDir()
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir = filepath.Join(tlsDir, "leaf")
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ca.IssueLeaf(leafDir, []string{host}); err != nil {
		t.Fatalf("IssueLeaf %s: %v", host, err)
	}
	return tlsDir, leafDir
}

// getAccepting issues a GET with an explicit Accept header. The retry page is
// gated on that header, so every assertion here needs to set it — which the
// shared httpGet, deliberately header-free, does not do.
func getAccepting(t *testing.T, client *http.Client, url, accept string) (int, http.Header, string, error) {
	t.Helper()
	return getWithHeaders(t, client, url, map[string]string{"Accept": accept})
}

// getWithHeaders issues a GET with the given request headers and drains the
// response, returning what the assertions here are written against: the status,
// the headers, and the body as a string.
func getWithHeaders(t *testing.T, client *http.Client, url string, headers map[string]string) (int, http.Header, string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Logf("close response body: %v", cerr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, "", err
	}
	return resp.StatusCode, resp.Header, string(body), nil
}
