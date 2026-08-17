// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/i18n"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// sitesOnly strips the retry-page snippet definitions out of a rendered config,
// leaving the global block and the site blocks.
//
// The snippets carry handle blocks and reverse-proxy-adjacent directives of
// their own, and they are emitted once at the top of every render — so a test
// asking "does this vhost have a handle block" has to ask about the vhost. An
// exact Replace of the bytes the renderer wrote, rather than a search for a
// marker, so this cannot silently stop stripping if the snippets change.
func sitesOnly(rendered string) string {
	return strings.Replace(rendered, unavailableSnippets(i18n.DefaultLocale), "", 1)
}

// The snippet definitions and the import lines are two spellings of the same
// three names, written in different places. If they drift, caddy fails to load
// the whole config — every vhost on the box, not just the one that is wrong.
func TestUnavailableSnippetsDefineTheNamesTheImportsUse(t *testing.T) {
	snips := unavailableSnippets(i18n.DefaultLocale)
	for _, name := range []string{
		snippetUnavailablePage, snippetUnavailableResponse, snippetUnavailableError,
	} {
		if def := "(" + name + ") {"; !strings.Contains(snips, def) {
			t.Errorf("the snippets do not define %q:\n%s", def, snips)
		}
	}

	var b strings.Builder
	writeUnavailableResponse(&b, "\t")
	writeUnavailableError(&b, "blog.asdf.home")
	imports := b.String()
	for _, want := range []string{
		"\timport " + snippetUnavailableResponse + "\n",
		"\timport " + snippetUnavailableError + " \"blog.asdf.home\"\n",
	} {
		if !strings.Contains(imports, want) {
			t.Errorf("import line %q missing from:\n%s", want, imports)
		}
	}

	// The page is imported ONCE, by the error snippet. The response snippet
	// raises a 503 into that same error handler instead of rendering its own
	// copy, and that is not a stylistic choice: every import of the page inlines
	// the whole thirty-row language map into the adapted JSON, so a second one
	// per proxy doubles the config caddy has to be handed on every reload.
	if n := strings.Count(snips, "import "+snippetUnavailablePage+" {args[0]}"); n != 1 {
		t.Errorf("want the page imported exactly once (by the error path), got %d:\n%s", n, snips)
	}
	if !strings.Contains(snips, "\t\t\terror 503\n") {
		t.Errorf("the 5xx path does not delegate to the site's error handler:\n%s", snips)
	}
}

// What the page actually tells the reader, and the three places the retry
// interval has to agree: the meta refresh the browser obeys, the Retry-After a
// well-behaved client obeys, and the sentence a person reads.
func TestUnavailablePageSaysItIsRetrying(t *testing.T) {
	snips := unavailableSnippets(i18n.DefaultLocale)
	for _, want := range []string{
		fmt.Sprintf(`<meta http-equiv="refresh" content="%d">`, unavailableRefreshSeconds),
		fmt.Sprintf("header Retry-After %d", unavailableRefreshSeconds),
		fmt.Sprintf("retries every %d seconds", unavailableRefreshSeconds),
		"<h1>{townos_title}</h1>",
		"<title>{townos_title}</title>",
		"{args[0]} is unavailable",
		"is not answering",
		"TOWNOS_UNAVAILABLE_HTML 503",
	} {
		if !strings.Contains(snips, want) {
			t.Errorf("the retry page is missing %q:\n%s", want, snips)
		}
	}

	// The service name reaches the page through the map's title, and the map's
	// title carries it as a snippet argument — never from the request. A page
	// that reflected the Host header would be an injection on the :80 fallback
	// block, which answers for every hostname a client cares to invent.
	//
	// One use per language row, one in the `default` row, and one on the error
	// snippet's import line — the hop that passes the label from the site block
	// into the page in the first place.
	wantLabels := len(unavailableLocales) + 2
	if n := strings.Count(snips, "{args[0]}"); n != wantLabels {
		t.Errorf("want the service label %d times (%d map rows, the default row, and the import that passes it in), got %d:\n%s",
			wantLabels, len(unavailableLocales), n, snips)
	}
	if strings.Contains(snips, "{http.request.host}") {
		t.Errorf("the retry page reflects the request Host into HTML:\n%s", snips)
	}
}

// A broken backend must not become a second, stranger failure in a caller that
// was parsing JSON. Only a browser-shaped request (GET/HEAD asking for HTML)
// gets the page; everything else gets the upstream's own response copied
// through, headers included.
func TestUnavailableSnippetsOnlyReplaceBrowserRequests(t *testing.T) {
	snips := unavailableSnippets(i18n.DefaultLocale)
	for _, want := range []string{
		"method GET HEAD",
		"header Accept *text/html*",
		"copy_response\n",
	} {
		if !strings.Contains(snips, want) {
			t.Errorf("the snippets are missing %q:\n%s", want, snips)
		}
	}

	// copy_response and NOT copy_response_headers. reverse_proxy has already
	// staged the upstream's headers by the time a response handler runs, so the
	// second directive emits every one of them twice — `Content-Length` included,
	// which is a malformed message rather than an untidy one. It reads like the
	// obvious companion to copy_response, which is exactly why it is pinned here.
	if strings.Contains(snips, "copy_response_headers") {
		t.Errorf("copy_response_headers duplicates every upstream header, Content-Length included:\n%s", snips)
	}
	// Both interception paths gate on the same request shape.
	if n := strings.Count(snips, "header Accept *text/html*"); n != 2 {
		t.Errorf("want both the response and error paths gated on Accept, got %d:\n%s", n, snips)
	}
}

// The two paths caddy can take to a broken backend, and the reason there are two
// snippets: an upstream 5xx is a response (handle_response, inside the proxy),
// and a backend that never answered is an error (handle_errors, at the site).
func TestUnavailableSnippetsCoverBothFailureShapes(t *testing.T) {
	snips := unavailableSnippets(i18n.DefaultLocale)
	for _, want := range []string{
		"@townos_unavailable_5xx status 5xx",
		"handle_response @townos_unavailable_5xx {",
		"handle_errors {",
	} {
		if !strings.Contains(snips, want) {
			t.Errorf("the snippets are missing %q:\n%s", want, snips)
		}
	}
	// A snippet that proxied anywhere would double every reverse_proxy in the
	// file, which is how the :80 contract (a package must never serve content
	// over plain HTTP) would stop being countable.
	if strings.Contains(snips, "reverse_proxy") {
		t.Errorf("the retry-page snippets must not proxy:\n%s", snips)
	}
}

// Every proxy in the file intercepts 5xx, and every site block that proxies
// catches a backend that never answered. One that did not would be a service
// whose failure the user sees as a raw caddy 502 with no retry.
func TestRenderCaddyfileEveryProxyGetsTheRetryPage(t *testing.T) {
	routes := []*ingresspb.Route{
		{
			Hostname: "blog.asdf.home", Backend: "town-os-system--pages:80",
			CertDir: "/c/blog", ServeHttp: true,
		},
		{
			Hostname: "gitea.asdf.home", Backend: "town-os-package--asdf-gitea-1.0:3000",
			CertDir: "/c/gitea",
		},
		{
			Hostname: "admin.asdf.home", Backend: "town-os-package--asdf-admin-1.0:8443",
			CertDir: "/c/admin", BackendTls: true,
		},
		{
			Hostname: "dns.asdf.home", Backend: "town-os-system--ui:80", CertDir: "/c/dns",
			PathBackends: []*ingresspb.PathBackend{
				{Path: "/dns-query", Backend: "127.0.0.2:443", BackendTls: true},
			},
		},
	}
	out := sitesOnly(string(renderCaddyfile(routes, 443, 80, "town-os-system--ui:80", false)))

	proxies := strings.Count(out, "reverse_proxy ")
	if got := strings.Count(out, "import "+snippetUnavailableResponse+"\n"); got != proxies {
		t.Errorf("%d reverse_proxy directives but %d 5xx interceptors:\n%s", proxies, got, out)
	}

	// One handle_errors import per site block that proxies: three HTTPS vhosts
	// plus the DoH one, the page's :80 vhost, and the :80 fallback.
	if got, want := strings.Count(out, "import "+snippetUnavailableError+" "), 6; got != want {
		t.Errorf("got %d site-level retry handlers, want %d:\n%s", got, want, out)
	}

	// A package's :80 block is a redirect and proxies nothing, so it gets
	// neither: there is no backend there to be unavailable.
	start := strings.Index(out, "\nhttp://gitea.asdf.home {")
	if start < 0 {
		t.Fatalf("the package's :80 redirect block is missing entirely:\n%s", out)
	}
	redir := out[start:]
	end := strings.Index(redir, "\n}\n")
	if end < 0 {
		t.Fatalf("the package's :80 redirect block is unterminated:\n%s", redir)
	}
	redir = redir[:end]
	if strings.Contains(redir, snippetUnavailableError) || strings.Contains(redir, snippetUnavailableResponse) {
		t.Errorf("the :80 redirect block carries a retry handler:\n%s", redir)
	}
}

// The page names the service the user asked for. For a route that is the
// hostname the renderer already validated as a DNS name; for the :80 fallback,
// which answers for every unmatched host, it is a constant — the Host header
// there is whatever the client sent.
func TestRenderCaddyfileRetryPageNamesTheService(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{{
		Hostname: "gitea.asdf.home", Backend: "b:3000", CertDir: "/c/gitea",
	}}, 443, 80, "town-os-system--ui:80", false))

	// Only the error import carries the name: it is the one that renders the
	// page. The response import raises a 503 into it and needs no label.
	for _, want := range []string{
		"import " + snippetUnavailableError + " \"gitea.asdf.home\"",
		"import " + snippetUnavailableError + " \"" + defaultBackendLabel + "\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "import "+snippetUnavailableResponse+" \"") {
		t.Errorf("the 5xx import took a service label it does not use:\n%s", out)
	}
}

// The label is a Caddyfile token and then the contents of an HTML element, so it
// is held to what both accept unescaped. Nothing should ever reach the fallback
// — every caller passes a validated hostname or the constant — and that is
// exactly why it is tested: it is the guard that keeps the claim true.
func TestSafeUnavailableLabel(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  string
	}{
		{"gitea.asdf.home", "gitea.asdf.home"},
		{"*.asdf.home", "*.asdf.home"},
		{"Town OS", "Town OS"},
		{"", genericUnavailableLabel},
		{"<script>alert(1)</script>", genericUnavailableLabel},
		{"a\"b", genericUnavailableLabel},
		{"a{b", genericUnavailableLabel},
		{"a}b", genericUnavailableLabel},
		{"a\nb", genericUnavailableLabel},
		{"a&b", genericUnavailableLabel},
		{strings.Repeat("a", 254), genericUnavailableLabel},
	} {
		if got := safeUnavailableLabel(tc.label); got != tc.want {
			t.Errorf("safeUnavailableLabel(%q) = %q, want %q", tc.label, got, tc.want)
		}
	}
}

// An injected hostname is dropped before it can be a label, so the page can
// never be the way a rejected route reaches the config.
func TestRenderCaddyfileDroppedRoutesGetNoRetryPage(t *testing.T) {
	out := string(renderCaddyfile([]*ingresspb.Route{{
		Hostname: "evil.home {\n\treverse_proxy evil:80\n}\nhttps://x.home",
		Backend:  "b:80",
		CertDir:  "/c/evil",
	}}, 443, 80, "", false))

	if strings.Contains(out, "evil") {
		t.Errorf("an injected hostname reached the config through a retry page:\n%s", out)
	}
	if strings.Contains(sitesOnly(out), "import ") {
		t.Errorf("a dropped route still imported a retry handler:\n%s", out)
	}
}

// Every language Town OS ships a catalog for gets a map row, every row names a
// catalog that exists, and every row's pattern compiles.
//
// Derived from i18n rather than listed twice: a language added to the catalog
// set and not to the map is a page that silently stays English for everybody who
// reads it, which is the one failure mode nobody testing in English can see. The
// check runs over every POPULATED locale — country variants included — because
// each of those has a base language that has to be answerable, even though the
// variants themselves share their base's row.
func TestUnavailableLocalesCoverEveryCatalog(t *testing.T) {
	rows := make(map[string]bool, len(unavailableLocales))
	for _, loc := range unavailableLocales {
		if !i18n.IsPopulated(loc.code) {
			t.Errorf("map row %q names a locale with no catalog", loc.code)
		}
		if rows[loc.code] {
			t.Errorf("the map has two rows for %q", loc.code)
		}
		rows[loc.code] = true

		// The pattern is compiled by caddy at load time, so a bad one is not a
		// wrong page — it is a config the ingress refuses to load, which takes
		// every vhost on the box with it.
		if _, err := regexp.Compile(loc.match); err != nil {
			t.Errorf("row %q has a pattern caddy cannot compile: %v", loc.code, err)
			continue
		}
		if !strings.HasPrefix(loc.match, "(?i)^") {
			t.Errorf("row %q is not anchored at the start of the header, case-insensitively: %q", loc.code, loc.match)
		}
	}

	for _, code := range i18n.PopulatedLocales() {
		base, _, _ := strings.Cut(code, "-")
		covered := false
		for row := range rows {
			if strings.HasPrefix(row, base+"-") || row == base {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("catalog %q has no retry-page row for its language (%q)", code, base)
		}
	}
}

// Traditional Chinese has to be matched before the generic zh row. caddy tests
// map rows in order and takes the first hit, so the wrong order serves
// Simplified to Taiwan, Hong Kong and Macau — a bug that looks like a working
// translation to everybody who does not read Chinese.
//
// Asserted against the patterns themselves as well as against their order,
// because "first match wins" is only safe if the narrow one is genuinely
// narrower.
func TestUnavailableSelectorPutsTraditionalChineseFirst(t *testing.T) {
	tw, cn := -1, -1
	for i, loc := range unavailableLocales {
		switch loc.code {
		case "zh-TW":
			tw = i
		case "zh-CN":
			cn = i
		}
	}
	if tw < 0 || cn < 0 {
		t.Fatalf("both Chinese rows must exist (zh-TW at %d, zh-CN at %d)", tw, cn)
	}
	if tw > cn {
		t.Fatalf("the zh-TW row is written after the generic zh one, so it can never match")
	}

	traditional := regexp.MustCompile(unavailableLocales[tw].match)
	simplified := regexp.MustCompile(unavailableLocales[cn].match)
	for _, header := range []string{"zh-TW,zh;q=0.9", "zh-HK", "zh-MO,zh", "zh-Hant-TW", "zh-tw"} {
		if !traditional.MatchString(header) {
			t.Errorf("Accept-Language %q is not read as Traditional Chinese", header)
		}
	}
	for _, header := range []string{"zh-CN,zh;q=0.9", "zh", "zh-Hans", "zh-SG"} {
		if traditional.MatchString(header) {
			t.Errorf("Accept-Language %q was claimed by the Traditional row", header)
		}
		if !simplified.MatchString(header) {
			t.Errorf("Accept-Language %q is not read as Simplified Chinese", header)
		}
	}

	// And a language row must not claim a tag that merely starts with its
	// letters: `^ja` without a boundary would answer Javanese in Japanese.
	japanese := regexp.MustCompile(unavailableLocales[indexOfLocale(t, "ja-JP")].match)
	for _, header := range []string{"jav", "java-x", "jam"} {
		if japanese.MatchString(header) {
			t.Errorf("the Japanese row claimed %q", header)
		}
	}
	for _, header := range []string{"ja", "ja-JP,en;q=0.8", "ja;q=0.9"} {
		if !japanese.MatchString(header) {
			t.Errorf("the Japanese row does not match %q", header)
		}
	}
}

// indexOfLocale returns the map row index for a locale code.
func indexOfLocale(t *testing.T, code string) int {
	t.Helper()
	for i, loc := range unavailableLocales {
		if loc.code == code {
			return i
		}
	}
	t.Fatalf("no map row for %q", code)
	return -1
}

// The page is actually translated: each branch carries its own catalog's
// sentences, not the English ones with a different lang attribute.
func TestUnavailablePageIsTranslatedPerLocale(t *testing.T) {
	snips := unavailableSnippets(i18n.DefaultLocale)
	for _, loc := range unavailableLocales {
		title := caddyHTMLText(fmt.Sprintf(i18n.T(loc.code, i18n.MsgIngressUnavailableTitle), "{args[0]}"))
		body := caddyHTMLText(i18n.T(loc.code, i18n.MsgIngressUnavailableBody))
		for _, want := range []string{title, body, `"` + loc.code + `"`} {
			if !strings.Contains(snips, want) {
				t.Errorf("%s: the selector does not carry %q:\n%s", loc.code, want, snips)
			}
		}
	}
	// Arabic is the one right-to-left catalog shipped, and the attribute is what
	// makes its punctuation land on the correct side of the sentence.
	if !strings.Contains(snips, `"ar-SA" "rtl"`) {
		t.Errorf("the Arabic branch is not marked right-to-left:\n%s", snips)
	}
	if strings.Contains(snips, `"en-US" "rtl"`) {
		t.Errorf("a left-to-right catalog was marked right-to-left:\n%s", snips)
	}
}

// The fallthrough branch — a client asking for a language Town OS ships nothing
// for — renders the box's configured locale, which is the same rule the UI
// follows: the browser's own preference first, the server's global setting only
// when there is no catalog for it.
func TestUnavailableFallbackFollowsTheConfiguredLocale(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured string
		wantCode   string
	}{
		{name: "a shipped language", configured: "de-DE", wantCode: "de-DE"},
		{name: "a country variant with its own catalog", configured: "es-MX", wantCode: "es-MX"},
		{name: "a locale nobody translated", configured: "xx-XX", wantCode: i18n.DefaultLocale},
		{name: "unset", configured: "", wantCode: i18n.DefaultLocale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snips := unavailableSnippets(tc.configured)
			// The fallthrough is the map's `default` row: the one with no
			// pattern, which is what makes it the fallthrough.
			idx := strings.Index(snips, "\n\t\tdefault ")
			if idx < 0 {
				t.Fatalf("the language map has no default row:\n%s", snips)
			}
			line := snips[idx+1:]
			if end := strings.Index(line, "\n"); end > 0 {
				line = line[:end]
			}
			if !strings.Contains(line, `"`+tc.wantCode+`"`) {
				t.Errorf("default row renders %q, want the %s catalog", line, tc.wantCode)
			}
			wantBody := caddyHTMLText(i18n.T(tc.wantCode, i18n.MsgIngressUnavailableBody))
			if !strings.Contains(line, wantBody) {
				t.Errorf("fallthrough does not carry the %s body:\n%s", tc.wantCode, line)
			}
		})
	}
}

// A translated string crosses two boundaries at once — it is a Caddyfile token
// and then the contents of an HTML element — and a catalog is a file people
// edit. Neither boundary may be crossable from inside a translation.
func TestCaddyHTMLText(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "plain text", want: "plain text"},
		{in: "{args[0]} is unavailable", want: "{args[0]} is unavailable"},
		{in: `a "quoted" word`, want: "a &#34;quoted&#34; word"},
		{in: "a <script> tag", want: "a &lt;script&gt; tag"},
		{in: "an & ampersand", want: "an &amp; ampersand"},
		{in: `a \ backslash`, want: "a &#92; backslash"},
		{in: "two\nlines", want: "two lines"},
	} {
		if got := caddyHTMLText(tc.in); got != tc.want {
			t.Errorf("caddyHTMLText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Nothing that survives may end a token or open an element.
	for _, loc := range unavailableLocales {
		for _, key := range []string{
			i18n.MsgIngressUnavailableTitle, i18n.MsgIngressUnavailableBody,
			i18n.MsgIngressUnavailableRetry, i18n.MsgIngressUnavailableFooter,
		} {
			got := caddyHTMLText(i18n.T(loc.code, key))
			if strings.ContainsAny(got, "\"<>\\\n\r") {
				t.Errorf("%s/%s escapes to something that still carries a metacharacter: %q", loc.code, key, got)
			}
		}
	}
}

// The retry page is written in Caddy's syntax, not in something that reads like
// it: snippets, a heredoc body, a response matcher, handle_response inside a
// proxy block, handle_errors outside one. This is the same parse `caddy reload`
// performs, and a config caddy rejects is not one bad vhost — it is every route
// on the box frozen at its last good state.
//
// Every route kind is in the config on purpose: the snippets are imported from
// six different places (an ACME vhost, a file-cert one, a TLS backend, a path
// backend, a page's :80 vhost, the :80 fallback), and each import expands in a
// slightly different surrounding block.
func TestRenderCaddyfileRetryPageValidatesWithCaddy(t *testing.T) {
	caddyBin := findCaddy(t)

	// Real leaves, not plausible-looking paths: validate provisions the TLS app
	// and opens every certificate the config names (see testLeafDir).
	content := renderCaddyfile([]*ingresspb.Route{
		{Hostname: "blog.asdf.home", Backend: "town-os-system--pages:80", CertDir: testLeafDir(t, "blog.asdf.home"), ServeHttp: true},
		{Hostname: "gitea.asdf.home", Backend: "town-os-package--asdf-gitea-1.0:3000", CertDir: testLeafDir(t, "gitea.asdf.home")},
		{Hostname: "git.example.com", Backend: "town-os-package--asdf-gitea-1.0:3000", Acme: true},
		{Hostname: "admin.asdf.home", Backend: "town-os-package--asdf-admin-1.0:8443", CertDir: testLeafDir(t, "admin.asdf.home"), BackendTls: true},
		{
			Hostname: "http.gfeh.home", Backend: "town-os-system--gfeh-home:9001", CertDir: testLeafDir(t, "http.gfeh.home"),
			PathBackends: []*ingresspb.PathBackend{{Path: "/", Backend: "town-os-system--pages:80"}},
		},
	}, 443, 80, "town-os-system--ui:80", false)

	path := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	out, err := exec.CommandContext(context.Background(), caddyBin,
		"validate", "--config", path, "--adapter", "caddyfile").CombinedOutput()
	if err != nil {
		t.Errorf("caddy rejected a config carrying the retry page: %v\n%s\n--- config ---\n%s",
			err, string(out), string(content))
	}
}

// The snippets are the same bytes on every render, so the supervisor can still
// no-op a reload whose content has not changed. They are also emitted for an
// empty route set: a snippet nothing imports is never parsed as directives, and
// a preamble that appears and disappears would bounce caddy on the first route.
func TestRenderCaddyfileSnippetsAreStable(t *testing.T) {
	empty := string(renderCaddyfile(nil, 443, 80, "", false))
	if !strings.Contains(empty, "("+snippetUnavailablePage+") {") {
		t.Errorf("the empty render dropped the retry-page snippets:\n%s", empty)
	}
	if sitesOnly(empty) == empty {
		t.Errorf("the snippets in the empty render are not the bytes unavailableSnippets returns:\n%s", empty)
	}

	withRoutes := string(renderCaddyfile([]*ingresspb.Route{
		{Hostname: "a.asdf.home", Backend: "b:80", CertDir: "/c/a"},
	}, 443, 80, "ui:80", false))
	if !strings.Contains(withRoutes, unavailableSnippets(i18n.DefaultLocale)) {
		t.Errorf("the snippets differ once routes are present:\n%s", withRoutes)
	}
}
