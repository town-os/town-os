// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"slices"
	"strings"
)

// The published-files index: the page http.gfeh.<tld> serves at its root.
//
// gfehd's http view answers exactly one route, /f/<token>, and 404s everything
// else — including "/". So the one name on the box whose entire purpose is
// handing files to people who were sent a link is also the one that tells a
// visitor who types it that there is nothing there. This page is what that root
// serves instead: the files the partition has actually published, each linked at
// the token it is served under.
//
// # It enumerates bearer credentials, and that is the point
//
// A /f/<token> URL is a capability: holding it is the whole of the
// authorization, which is why the partition index at gfeh.<tld> lists views and
// deliberately lists no exposures (see index.go). This page lists them, and the
// consequence is not subtle — publishing a file stops meaning "reachable by
// somebody I sent the link to" and starts meaning "listed to anyone who can
// resolve the name". That is the trade this page is: a directory of published
// files is worth having, and a directory that omitted the links would be a list
// of filenames nobody could open.
//
// What follows from it is that *withdrawing* is now the only thing that makes a
// file private again. An exposure that is disabled contributes no row, because a
// row would advertise a link that resolves to nothing.

// PublicFile is one row: a published file and the token it is served at.
type PublicFile struct {
	// Name is what the file is called, for a reader.
	Name string
	// Token is the /f/<token> segment the file is served under.
	Token string
}

// PublicIndexPage is everything the published-files index renders from.
type PublicIndexPage struct {
	// Network is the Town OS network the partition belongs to.
	Network string
	// FQDN is the name this page is served under, which is also the name every
	// link on it resolves under.
	FQDN string
	// Files is what has been published, in whatever order; the renderer sorts.
	Files []PublicFile
}

// PublicFilesFromExposures turns a partition's exposures into index rows.
//
// Disabled exposures are dropped: gfehd does not serve them, so a row would be a
// link that 404s — which reads as a broken index rather than a withdrawn file.
//
// The name is the exposure's filename, falling back to the last element of its
// path and then to the token itself. The fallbacks exist because filename is
// optional on gfehd's side and a row has to say *something*; a token is a poor
// name but it is at least the truth about what the link is.
func PublicFilesFromExposures(exposures []Exposure) []PublicFile {
	out := make([]PublicFile, 0, len(exposures))
	for _, e := range exposures {
		if !e.Enabled || e.Token == "" {
			continue
		}
		out = append(out, PublicFile{Name: publicFileName(e), Token: e.Token})
	}
	SortPublicFiles(out)
	return out
}

// publicFileName picks the label for one exposure.
func publicFileName(e Exposure) string {
	if e.Filename != nil {
		if name := strings.TrimSpace(*e.Filename); name != "" {
			return name
		}
	}
	if p := strings.TrimSpace(e.Path); p != "" {
		if base := path.Base(p); base != "." && base != "/" {
			return base
		}
	}
	return e.Token
}

// SortPublicFiles orders rows by name, then by token.
//
// A fixed order rather than gfehd's: the rendered page is compared against what
// is on disk to decide whether to write, so an order that varied between
// reconciles would rewrite the file every pass. The token breaks the tie because
// two published files may legitimately share a name — the same file published
// twice, or two files of the same name from different directories — and a
// comparison that called those equal would leave their order to the sort's
// stability and the daemon's.
func SortPublicFiles(files []PublicFile) {
	slices.SortFunc(files, func(a, b PublicFile) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Token, b.Token)
	})
}

// PublishedPath is the path a token is served at, relative to the http view's
// own name.
//
// Relative, and deliberately: the page is served under exactly the name the
// links resolve under, so a root-relative path is correct by construction and
// cannot name a host the ingress does not route — which an absolute URL composed
// from a TLD this package does not know would be free to do.
func PublishedPath(token string) string {
	return "/f/" + url.PathEscape(token)
}

// RenderPublicIndex renders the page.
//
// Self-contained for the same reason RenderIndex is: it is served by the pages
// Caddy container, which has no egress a browser would follow to a CDN. Inline
// CSS, no script, and deterministic for a given input so the reconcile writes
// only when the content actually changed.
func RenderPublicIndex(page PublicIndexPage) []byte {
	files := slices.Clone(page.Files)
	SortPublicFiles(files)

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString("Published files — "+page.Network))
	b.WriteString(indexStyle)
	b.WriteString("</head>\n<body>\n<main>\n")

	fmt.Fprintf(&b, "<h1>Published files</h1>\n<p class=\"sub\">Shared out of partition <code>%s</code>.</p>\n",
		html.EscapeString(page.Network))

	if len(files) == 0 {
		b.WriteString("<p class=\"empty\">Nothing has been published from this partition yet.</p>\n")
	} else {
		b.WriteString("<ul class=\"views\">\n")
		for _, f := range files {
			writePublicFileRow(&b, f)
		}
		b.WriteString("</ul>\n")
	}

	b.WriteString("<p class=\"note\">Every file listed here is served to anyone who can reach this name. " +
		"Withdraw a link from the Town OS dashboard, under Object Storage, to take a file off this page.</p>\n")
	b.WriteString("</main>\n</body>\n</html>\n")
	return []byte(b.String())
}

// writePublicFileRow renders one file.
func writePublicFileRow(b *strings.Builder, f PublicFile) {
	link := PublishedPath(f.Token)
	b.WriteString("<li>\n")
	fmt.Fprintf(b, "<h2>%s</h2>\n", html.EscapeString(f.Name))
	fmt.Fprintf(b, "<p class=\"url\"><a href=\"%s\">%s</a></p>\n", html.EscapeString(link), html.EscapeString(link))
	b.WriteString("</li>\n")
}
