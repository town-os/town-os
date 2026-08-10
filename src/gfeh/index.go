// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"fmt"
	"html"
	"slices"
	"strings"
)

// The partition index: one page, per partition, that says what its views are
// and where to point a client at them.
//
// Every view gfeh serves is a *protocol* endpoint, and none of them is
// browsable. The HTTP view has exactly one route, /f/{token}, so its root is a
// 404; S3 answers an XML error to a request it cannot parse as an S3 operation;
// the Drive and IPFS views answer their own APIs. So an operator who has just
// been told their box has object storage, and who does the one thing anybody
// does — opens the name in a browser — is told the thing is broken. It is not:
// there was never anywhere to look.
//
// The index is that place. It is published under the label every view label is
// already a child of, so it needs no name of its own that anybody has to learn:
// the views are s3.gfeh, http.gfeh, drive.gfeh, ipfs.gfeh, and the index is
// gfeh.
//
// What it deliberately does NOT carry: exposures, principals, grants, quota, or
// anything else read off the admin socket. It is served by the pages static
// server with no authentication in front of it, so it can only hold what is
// already public — and every published /f/<token> link is a bearer credential,
// which is exactly the thing an unauthenticated index must never enumerate. It
// lists the *views*, which are already in DNS.

// IndexLabel is the label a partition's index is published under: the parent of
// every view label, since gfehctl names views "<view>.gfeh" (viewLabel).
//
// Deriving it from VolumePrefix rather than writing "gfeh" a second time is the
// point — the two must be the same string, or the index lands somewhere that is
// not the parent of the views it indexes.
const IndexLabel = VolumePrefix

// ViewIndex is the view name Town OS files the index site under.
//
// Deliberately not in HTTPViews and therefore not accepted by IsHTTPView: that
// predicate answers "is this a view gfehd reported that the ingress can front",
// and the index is neither reported by gfehd nor served by it. It is served by
// the pages container, and the site that carries it sets HTTP explicitly.
const ViewIndex = "index"

// IndexView is one row of the index: a view, and where a client reaches it.
type IndexView struct {
	// View is s3, http, drive or ipfs.
	View string
	// FQDN is the name the view is served under.
	FQDN string
	// URL is what a client actually dials.
	//
	// Note this is NOT derived from the port gfehd reported. For the four HTTP
	// views that port is a container-side backend port the ingress proxies to,
	// and publishing https://s3.gfeh.home:9000 would give every reader an
	// address that refuses the connection. The ingress answers on :443, so the
	// URL carries no port at all.
	URL string
}

// IndexPage is everything the index renders from.
type IndexPage struct {
	// Network is the Town OS network this partition belongs to.
	Network string
	// TLD is the zone its names are published under.
	TLD string
	// Views is what the partition is serving, in HTTPViews order.
	Views []IndexView
}

// viewDescriptions says what each view is, in one sentence, to somebody who has
// just arrived and does not know what gfeh is.
//
// Kept here rather than in the UI catalog because the index is a static page
// served by Caddy with no access to the UI's i18n bundle. It is English-only
// for that reason, which is a real limitation and the reason the page links
// back to the dashboard, which is translated.
var viewDescriptions = map[string]string{
	ViewS3: "S3-compatible object API. Point an S3 client at this endpoint using an access key issued for this partition; the bucket is the partition.",
	ViewHTTP: "Published links. A file shared out of this partition is served at /f/<token> under this name — that link, and nothing else, is what this view answers. " +
		"The dashboard's Links tab lists the ones that exist.",
	ViewDrive: "Google Drive API view, for clients that speak Drive. Authenticates with a bearer token issued for this partition.",
	ViewIPFS:  "IPFS gateway and API. Content published out of this partition is addressable by CID through this endpoint.",
	ViewSMB:   "SMB share. Town OS does not serve this view.",
}

// ViewDescription returns the one-line description of a view, or the empty
// string for one this build does not know about.
//
// An unknown view renders with no description rather than with a placeholder:
// gfehd is free to grow a view before Town OS learns what it is, and a row that
// names it and its URL is still useful, while "unknown view" beside a working
// endpoint reads as a fault.
func ViewDescription(view string) string { return viewDescriptions[view] }

// SortIndexViews orders rows the way HTTPViews does, with anything unrecognized
// after them in name order.
//
// A fixed order rather than whatever the daemon happened to answer with: the
// rendered page is compared against what is on disk to decide whether to write,
// so an order that varied between reconciles would rewrite the file every pass.
func SortIndexViews(views []IndexView) {
	slices.SortFunc(views, func(a, b IndexView) int {
		ai, bi := slices.Index(HTTPViews, a.View), slices.Index(HTTPViews, b.View)
		if ai != bi {
			// -1 (unrecognized) must sort last, not first.
			if ai < 0 {
				return 1
			}
			if bi < 0 {
				return -1
			}
			return ai - bi
		}
		return strings.Compare(a.FQDN, b.FQDN)
	})
}

// RenderIndex renders the page.
//
// Self-contained by necessity, not by preference: it is served by the pages
// Caddy container, which has no network egress path a browser would follow to a
// CDN, and a box whose whole point is running from RAM on a home LAN should not
// need the internet to render a status page. So the CSS is inline and there is
// no script at all.
//
// Deterministic for a given input, which is what lets the reconcile path write
// only when the content changed rather than touching the file every pass.
func RenderIndex(page IndexPage) []byte {
	views := slices.Clone(page.Views)
	SortIndexViews(views)

	var b strings.Builder
	title := "Object storage — " + page.Network

	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	b.WriteString(indexStyle)
	b.WriteString("</head>\n<body>\n<main>\n")

	fmt.Fprintf(&b, "<h1>Object storage</h1>\n<p class=\"sub\">Partition <code>%s</code>, published under <code>%s</code>.</p>\n",
		html.EscapeString(page.Network), html.EscapeString(page.TLD))

	if len(views) == 0 {
		// Reachable: a partition whose daemon is up but serving no HTTP view.
		// Saying so beats an empty page, which reads as a rendering failure.
		b.WriteString("<p class=\"empty\">This partition is serving no browsable views.</p>\n")
	} else {
		b.WriteString("<ul class=\"views\">\n")
		for _, v := range views {
			writeIndexRow(&b, v)
		}
		b.WriteString("</ul>\n")
	}

	// Said unconditionally, and with no link. Every name above is served with a
	// leaf from this box's own CA, so a client that has not been given the root
	// refuses or warns on all of them — and the reader of this page is usually
	// about to configure exactly such a client. There is no link because the CA
	// is served by the system controller on its own port, which this page has no
	// reliable way to name: it is rendered by the reconcile, which knows the
	// box's LAN address but not which port the controller was told to bind or
	// whether it is serving TLS. A link that is wrong on some boxes is worse
	// than a sentence that is right on all of them.
	b.WriteString("<p class=\"note\">These names are served with this box's own certificate authority. " +
		"A client that has not been given the root certificate will refuse or warn on every one of them; " +
		"the Town OS dashboard serves it at <code>/tls/ca.crt</code>.</p>\n")
	b.WriteString("<p class=\"note\">Users, permissions and published links are managed from the Town OS dashboard, under Object Storage.</p>\n")
	b.WriteString("</main>\n</body>\n</html>\n")
	return []byte(b.String())
}

// writeIndexRow renders one view.
func writeIndexRow(b *strings.Builder, v IndexView) {
	b.WriteString("<li>\n")
	fmt.Fprintf(b, "<h2>%s</h2>\n", html.EscapeString(v.View))
	if v.URL != "" {
		fmt.Fprintf(b, "<p class=\"url\"><a href=\"%s\">%s</a></p>\n", html.EscapeString(v.URL), html.EscapeString(v.URL))
	} else {
		fmt.Fprintf(b, "<p class=\"url\"><code>%s</code></p>\n", html.EscapeString(v.FQDN))
	}
	if desc := ViewDescription(v.View); desc != "" {
		fmt.Fprintf(b, "<p class=\"desc\">%s</p>\n", html.EscapeString(desc))
	}
	b.WriteString("</li>\n")
}

// indexStyle is the whole of the page's presentation. Both colour schemes are
// styled because the page has no toggle and no storage to remember one in — the
// browser's preference is the only signal there is.
const indexStyle = `<style>
:root { color-scheme: light dark; --fg: #16181d; --muted: #5c6370; --bg: #fbfbfc; --card: #ffffff; --line: #e3e5ea; --link: #1c5fd0; }
@media (prefers-color-scheme: dark) {
  :root { --fg: #e6e8ec; --muted: #9aa1ad; --bg: #14161a; --card: #1b1e24; --line: #2b2f37; --link: #7fb0ff; }
}
* { box-sizing: border-box; }
body { margin: 0; padding: 2rem 1rem; background: var(--bg); color: var(--fg);
  font: 16px/1.55 ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
main { max-width: 46rem; margin: 0 auto; }
h1 { font-size: 1.5rem; margin: 0 0 .25rem; }
h2 { font-size: 1rem; margin: 0 0 .35rem; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); }
p { margin: 0 0 .5rem; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: .9em; }
a { color: var(--link); }
.sub { color: var(--muted); margin-bottom: 1.5rem; }
.views { list-style: none; margin: 0; padding: 0; display: grid; gap: .75rem; }
.views li { background: var(--card); border: 1px solid var(--line); border-radius: .5rem; padding: .9rem 1rem; }
.url { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; word-break: break-all; }
.desc { color: var(--muted); font-size: .925rem; margin: 0; }
.note, .empty { color: var(--muted); font-size: .9rem; margin-top: 1.5rem; }
</style>
`
