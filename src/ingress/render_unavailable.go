// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"fmt"
	"html"
	"strings"

	"gitea.com/town-os/town-os/src/i18n"
)

// unavailableRefreshSeconds is how long the retry page waits before reloading
// itself, and the Retry-After it sends with the 503.
//
// Five seconds, because the thing it is waiting for is a container: a package
// that is restarting after an upgrade, or one whose image was just pulled and
// whose first boot takes a few seconds. A minute would make a service that came
// back immediately look dead for most of that minute, and a second would put a
// browser in a hot loop against a box that is already busy starting something.
const unavailableRefreshSeconds = 5

// The three Caddyfile snippets the renderer defines once and every site block
// imports. Constants because they are written in two places that have to agree
// — the snippet definitions and the import lines — and a typo in either is a
// config caddy refuses to load, which takes every vhost on the box down with it.
// TestUnavailableSnippetsDefineTheNamesTheImportsUse holds the two together.
const (
	snippetUnavailablePage     = "townos_unavailable_page"
	snippetUnavailableResponse = "townos_unavailable_response"
	snippetUnavailableError    = "townos_unavailable_error"
)

// defaultBackendLabel is what the retry page calls the :80 fallback backend.
//
// The fallback answers for every host that matched no route, so there is no one
// hostname to name — and naming the requested one is not an option: the Host
// header on that block is whatever the client sent, and pasting it into the page
// would reflect script into HTML on any request somebody cared to craft. Every
// other label the page renders is a route hostname this renderer already
// validated as a DNS name.
const defaultBackendLabel = "Town OS"

// genericUnavailableLabel stands in for a label that could not be pasted into
// HTML safely. Nothing should ever reach it — every caller passes either a
// validated hostname or the constant above — but it is the difference between a
// future caller producing a bland page and producing an injection.
const genericUnavailableLabel = "This service"

// unavailableLocale is one row of the retry page's language map: a regular
// expression over Accept-Language, the catalog that answers it, and the
// direction its script runs in.
type unavailableLocale struct {
	// match is the regular expression caddy tests Accept-Language against,
	// without its leading `~`. It is anchored at the start of the header, so it
	// matches the client's FIRST (most preferred) language — the one it would
	// rather read — and it is case-insensitive because a tag's region subtag is
	// conventionally uppercase and nothing enforces that.
	match string
	// code is the i18n catalog to render, and the page's lang attribute.
	code string
	// rtl marks a right-to-left script, which becomes the dir attribute.
	rtl bool
}

// unavailableLocales is the language map, in the order the rows are written.
// caddy tests them in order and takes the first match, and that order is
// load-bearing exactly once: the Traditional Chinese row has to precede the
// generic zh one, since `^zh` otherwise claims zh-TW and serves Simplified to
// Taiwan, Hong Kong and Macau.
//
// One row per LANGUAGE, not per locale. Country variants (de-AT, en-GB,
// es-MX…) derive from their base language and override only the strings that
// country genuinely says differently — none of which are these four — so a row
// each would be thirty more regexes rendering the same page.
//
// Each pattern ends at a separator or the end of the header, so `^ja` cannot
// claim a tag that merely starts with those two letters.
//
// TestUnavailableLocalesCoverEveryCatalog derives the expected membership from
// i18n itself and fails in both directions, so a language added to the catalog
// set cannot quietly go unserved here.
var unavailableLocales = []unavailableLocale{
	{match: `(?i)^zh-(TW|HK|MO|Hant)`, code: "zh-TW"},
	{match: `(?i)^zh([-,;]|$)`, code: "zh-CN"},
	{match: `(?i)^ar([-,;]|$)`, code: "ar-SA", rtl: true},
	{match: `(?i)^bn([-,;]|$)`, code: "bn-BD"},
	{match: `(?i)^cs([-,;]|$)`, code: "cs-CZ"},
	{match: `(?i)^da([-,;]|$)`, code: "da-DK"},
	{match: `(?i)^de([-,;]|$)`, code: "de-DE"},
	{match: `(?i)^en([-,;]|$)`, code: "en-US"},
	{match: `(?i)^es([-,;]|$)`, code: "es-ES"},
	{match: `(?i)^fi([-,;]|$)`, code: "fi-FI"},
	{match: `(?i)^fr([-,;]|$)`, code: "fr-FR"},
	{match: `(?i)^hi([-,;]|$)`, code: "hi-IN"},
	{match: `(?i)^hr([-,;]|$)`, code: "hr-HR"},
	{match: `(?i)^hu([-,;]|$)`, code: "hu-HU"},
	{match: `(?i)^it([-,;]|$)`, code: "it-IT"},
	{match: `(?i)^ja([-,;]|$)`, code: "ja-JP"},
	{match: `(?i)^ko([-,;]|$)`, code: "ko-KR"},
	{match: `(?i)^nl([-,;]|$)`, code: "nl-NL"},
	{match: `(?i)^pl([-,;]|$)`, code: "pl-PL"},
	{match: `(?i)^pt([-,;]|$)`, code: "pt-BR"},
	{match: `(?i)^ro([-,;]|$)`, code: "ro-RO"},
	{match: `(?i)^ru([-,;]|$)`, code: "ru-RU"},
	{match: `(?i)^sa([-,;]|$)`, code: "sa-IN"},
	{match: `(?i)^sk([-,;]|$)`, code: "sk-SK"},
	{match: `(?i)^sl([-,;]|$)`, code: "sl-SI"},
	{match: `(?i)^sv([-,;]|$)`, code: "sv-SE"},
	{match: `(?i)^th([-,;]|$)`, code: "th-TH"},
	{match: `(?i)^tr([-,;]|$)`, code: "tr-TR"},
	{match: `(?i)^uk([-,;]|$)`, code: "uk-UA"},
	{match: `(?i)^vi([-,;]|$)`, code: "vi-VN"},
}

// unavailableSnippets returns the Caddyfile snippet definitions that implement
// the retry page, with defaultLocale as the language served to a client whose
// own language Town OS does not ship. They are emitted once per render, right
// after the global options block, and imported by every site block that proxies
// anywhere.
//
// Three snippets, and each boundary is there for a reason:
//
//   - townos_unavailable_page is the page: a `map` over Accept-Language that
//     selects a language, followed by ONE copy of the HTML reading what the map
//     produced. A map rather than a handle block per catalog because the block
//     form inlines the whole page thirty times — see the note on size below.
//   - townos_unavailable_response and townos_unavailable_error are the two ways
//     caddy reaches a broken backend, and only one of them is a "response":
//     the backend answered with a 5xx (an upstream response, intercepted with a
//     response matcher and handle_response INSIDE the reverse_proxy block), or
//     the backend never answered at all — container down, name unresolvable,
//     connection refused — where reverse_proxy fails and caddy raises its own
//     error, which handle_response never sees. That is handle_errors, at site
//     level. The second is the common one on a home box, and the one an
//     implementation that only wrote handle_response would silently miss,
//     because the 5xx it is looking for never comes from anywhere.
//
// The response path does not render the page. It raises `error 503`, which
// falls through to the site's own handle_errors — so the page, and the thirty
// rows of language behind it, are written ONCE per site block rather than once
// per proxy. That is a size decision with teeth: an import is textual, so every
// expansion is a full copy in the JSON caddy is handed on every reload, and a
// box with twenty routes reloads on every package install, page edit and hourly
// reconcile.
//
// Both paths serve the page only to a client that asked for HTML with a GET or
// HEAD. An API client — an XHR, a webhook, a `curl` — gets the upstream's own
// response copied through verbatim on the response path, and a plain 503 on the
// error path. Handing an HTML page to something parsing JSON turns one broken
// backend into a second, stranger failure in the caller.
//
// That passthrough is `copy_response` ALONE, with no copy_response_headers
// beside it, and the omission is deliberate: reverse_proxy has already staged
// the upstream's headers on the response writer by the time a response handler
// runs, so copying them again emits every one of them twice — including
// `Content-Length`, which makes the message malformed rather than merely
// untidy. Verified against the caddy binary the ingress runs: without the
// directive the status, body, Content-Type and any custom header all arrive
// exactly once; with it, all of them arrive twice.
//
// handle_errors carries no status matcher because inside these site blocks it
// cannot fire for anything else: every request is either proxied or handled by
// a handle block, so the only error caddy can raise here is the proxy failing.
func unavailableSnippets(defaultLocale string) string {
	var b strings.Builder
	b.WriteString("\n(" + snippetUnavailablePage + ") {\n")
	fmt.Fprintf(&b, "\tmap {header.Accept-Language} %s {\n", strings.Join(unavailableVars, " "))
	for _, loc := range unavailableLocales {
		writeUnavailableMapRow(&b, "~"+loc.match, loc)
	}
	// The default row: a client whose language Town OS ships nothing for, and a
	// client that stated no language at all. It gets the box's configured locale
	// rather than English outright, which is the same rule the UI follows — the
	// browser's own preference first, the server's global setting only when
	// there is no catalog for it.
	writeUnavailableMapRow(&b, "default", fallbackUnavailableLocale(defaultLocale))
	b.WriteString("\t}\n")
	b.WriteString("\theader Content-Type \"text/html; charset=utf-8\"\n")
	b.WriteString("\theader Cache-Control \"no-store\"\n")
	fmt.Fprintf(&b, "\theader Retry-After %d\n", unavailableRefreshSeconds)
	b.WriteString(unavailablePageTemplate)
	b.WriteString("}\n")

	fmt.Fprintf(&b, unavailableWrapperTemplate,
		snippetUnavailableResponse,
		snippetUnavailableError, snippetUnavailablePage, unavailableRefreshSeconds)
	return b.String()
}

// unavailableVars are the map's output placeholders, in the order the rows
// supply them and the order the page reads them.
var unavailableVars = []string{
	"{townos_lang}", "{townos_dir}", "{townos_title}",
	"{townos_body}", "{townos_retry}", "{townos_footer}",
}

// fallbackUnavailableLocale resolves the configured default into the map's
// `default` row, falling back to en-US for a code with no catalog.
//
// A locale nobody translated is not an error worth failing a render over — the
// setting is a free-text row in the settings table, and the cost of a bad one
// should be an English page, not an ingress that will not start.
func fallbackUnavailableLocale(code string) unavailableLocale {
	if code == "" || !i18n.IsPopulated(code) {
		return unavailableLocale{code: i18n.DefaultLocale}
	}
	for _, loc := range unavailableLocales {
		if loc.code == code {
			return loc
		}
	}
	// A populated country variant (de-AT, es-MX, en-GB…). It has its own
	// catalog, so render from it; direction follows the language it derives
	// from, which is what the base-language row already records.
	base, _, _ := strings.Cut(code, "-")
	for _, loc := range unavailableLocales {
		if strings.HasPrefix(loc.code, base+"-") {
			return unavailableLocale{code: code, rtl: loc.rtl}
		}
	}
	return unavailableLocale{code: code}
}

// writeUnavailableMapRow emits one row of the language map: the input caddy
// matches (a `~`-prefixed regex, or `default`) followed by the six values the
// page reads, in unavailableVars order.
//
// The title carries the service label as `{args[0]}` — the argument of the
// snippet being imported, substituted when a site block imports it. That is why
// the label is a format verb in the catalog rather than something this code
// concatenates: "%s is unavailable" does not put the name in the same place in
// every language, and a page that read "は利用できません blog.home" would be a
// translation in name only.
func writeUnavailableMapRow(b *strings.Builder, input string, loc unavailableLocale) {
	dir := "ltr"
	if loc.rtl {
		dir = "rtl"
	}
	fmt.Fprintf(b, "\t\t%s %q %q %q %q %q %q\n",
		input, loc.code, dir,
		caddyHTMLText(fmt.Sprintf(i18n.T(loc.code, i18n.MsgIngressUnavailableTitle), "{args[0]}")),
		caddyHTMLText(i18n.T(loc.code, i18n.MsgIngressUnavailableBody)),
		caddyHTMLText(fmt.Sprintf(i18n.T(loc.code, i18n.MsgIngressUnavailableRetry), unavailableRefreshSeconds)),
		caddyHTMLText(i18n.T(loc.code, i18n.MsgIngressUnavailableFooter)))
}

// caddyHTMLText makes a translated string safe to be both a Caddyfile token and
// the contents of an HTML element.
//
// Two boundaries, one pass. HTML escaping handles the first four characters
// that matter and also removes the quote that would end the token early; the
// backslash is escaped separately because Caddy reads it as an escape inside a
// quoted token and Go's html package has no reason to touch it. Newlines and
// control characters are turned into spaces: neither can appear in a token at
// all, and a catalog is a file people edit.
//
// `{args[0]}` survives on purpose — the braces are not HTML-escaped, and that
// placeholder is how the service name reaches the page.
func caddyHTMLText(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "\\", "&#92;")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == 0 {
			return ' '
		}
		return r
	}, s)
}

// unavailablePageTemplate is the page's body, a heredoc because it is HTML.
//
// The CSS braces in it survive for a reason worth knowing: caddy substitutes
// placeholders in a respond body with ReplaceKnown, which leaves an
// unrecognized `{...}` exactly as written. `{args[N]}` is not a runtime
// placeholder at all — it is a snippet argument, substituted when the selector
// imports this template, which is what lets one page render in thirty languages
// and name a different service on every vhost without templating anything at
// request time.
var unavailablePageTemplate = fmt.Sprintf(`	respond <<TOWNOS_UNAVAILABLE_HTML
		<!DOCTYPE html>
		<html lang="{townos_lang}" dir="{townos_dir}">
		<head>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<meta http-equiv="refresh" content="%d">
		<title>{townos_title}</title>
		<style>
		:root { color-scheme: light dark; }
		body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center; font-family: system-ui, sans-serif; }
		main { max-width: 32rem; padding: 2rem; text-align: center; }
		h1 { font-size: 1.4rem; margin: 0 0 1rem; }
		p { margin: 0 0 0.75rem; line-height: 1.5; }
		.muted { opacity: 0.65; font-size: 0.9rem; }
		</style>
		</head>
		<body>
		<main>
		<h1>{townos_title}</h1>
		<p>{townos_body}</p>
		<p>{townos_retry}</p>
		<p class="muted">{townos_footer}</p>
		</main>
		</body>
		</html>
		TOWNOS_UNAVAILABLE_HTML 503
`, unavailableRefreshSeconds)

// unavailableWrapperTemplate is the two interception snippets. Its verbs are,
// in order: the response snippet's name, the page snippet it calls, the error
// snippet's name, the page snippet again, and the Retry-After for the plain
// answer an API client gets when the backend never answered.
//
// That last answer is the one string here that is NOT translated, deliberately:
// it is the body of a 503 to something that told us it does not want HTML — an
// XHR, a webhook, a resolver — where the status code and Retry-After are the
// message and the text is a courtesy for whoever reads the log. Everything a
// person reads goes through the catalog.
const unavailableWrapperTemplate = `
(%s) {
	@townos_unavailable_5xx status 5xx
	handle_response @townos_unavailable_5xx {
		@townos_unavailable_wants_html {
			method GET HEAD
			header Accept *text/html*
		}
		handle @townos_unavailable_wants_html {
			error 503
		}
		handle {
			copy_response
		}
	}
}

(%s) {
	handle_errors {
		@townos_unavailable_error_wants_html {
			method GET HEAD
			header Accept *text/html*
		}
		handle @townos_unavailable_error_wants_html {
			import %s {args[0]}
		}
		handle {
			header Retry-After %d
			respond "This service is unavailable. Town OS is still routing to it; retry shortly." 503
		}
	}
}
`

// writeUnavailableResponse emits the import that turns an upstream 5xx into the
// retry page. It belongs inside a reverse_proxy block: the response matcher it
// defines is a reverse_proxy subdirective and means nothing anywhere else.
//
// It takes no service label, because it does not render the page: it raises a
// 503 that falls through to the site's handle_errors, which does. That is what
// keeps the page — and the thirty-row language map behind it — expanded ONCE
// per site block rather than once per proxy.
func writeUnavailableResponse(b *strings.Builder, indent string) {
	fmt.Fprintf(b, "%simport %s\n", indent, snippetUnavailableResponse)
}

// writeUnavailableError emits the import that turns a proxy failure — a backend
// that never answered — into the retry page.
//
// No indent parameter, unlike its response counterpart: handle_errors is a
// site-block directive, so there is exactly one depth it can be written at, and
// a caller that passed anything else would be writing it somewhere caddy does
// not accept it.
func writeUnavailableError(b *strings.Builder, label string) {
	fmt.Fprintf(b, "\timport %s %q\n", snippetUnavailableError, safeUnavailableLabel(label))
}

// safeUnavailableLabel returns a label that can be pasted into the page's HTML,
// or the generic stand-in when it cannot.
//
// The label crosses two boundaries at once — it is a Caddyfile token and then
// the contents of an HTML element — so it is held to what both accept without
// escaping: DNS-name characters plus spaces, for the one label that is a phrase
// rather than a hostname. Route hostnames have already been through
// validSiteHost by the time they arrive here; this is the guard that keeps that
// true if a future caller labels a page with something else.
func safeUnavailableLabel(label string) string {
	if label == "" || len(label) > 253 {
		return genericUnavailableLabel
	}
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_', r == '*', r == ' ':
		default:
			return genericUnavailableLabel
		}
	}
	return label
}
