// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"slices"
	"strings"
	"testing"
)

func samplePage() IndexPage {
	return IndexPage{
		Network: "home",
		TLD:     "home",
		Views: []IndexView{
			{View: ViewIPFS, FQDN: "ipfs.gfeh.home", URL: "https://ipfs.gfeh.home"},
			{View: ViewS3, FQDN: "s3.gfeh.home", URL: "https://s3.gfeh.home"},
			{View: ViewHTTP, FQDN: "http.gfeh.home", URL: "https://http.gfeh.home"},
		},
	}
}

// The index label must be the parent of every view label, or the index is not
// an index of anything -- it is a fifth name beside the four.
func TestIndexLabelIsTheParentOfEveryViewLabel(t *testing.T) {
	for _, view := range HTTPViews {
		label := view + "." + VolumePrefix
		if !strings.HasSuffix(label, "."+IndexLabel) {
			t.Errorf("view label %q is not a child of the index label %q", label, IndexLabel)
		}
	}
}

// The index is not a view gfehd reports, so it must never be mistaken for one:
// IsHTTPView gates the ingress route, and a site claiming to be a gfeh view
// would be given a backend on the gfeh container, where nothing serves it.
func TestViewIndexIsNotAReportedView(t *testing.T) {
	if IsHTTPView(ViewIndex) {
		t.Error("IsHTTPView accepts the index pseudo-view")
	}
	if slices.Contains(HTTPViews, ViewIndex) {
		t.Error("HTTPViews contains the index pseudo-view")
	}
}

// The rendered bytes decide whether the reconcile writes, so an unstable render
// would rewrite the file on every pass.
func TestRenderIndexIsDeterministic(t *testing.T) {
	first := RenderIndex(samplePage())
	for range 5 {
		if got := RenderIndex(samplePage()); string(got) != string(first) {
			t.Fatal("RenderIndex is not deterministic for one input")
		}
	}

	// And independent of the order the daemon happened to answer in.
	shuffled := samplePage()
	shuffled.Views = []IndexView{shuffled.Views[2], shuffled.Views[0], shuffled.Views[1]}
	if string(RenderIndex(shuffled)) != string(first) {
		t.Error("RenderIndex depends on the order of its input views")
	}
}

// RenderIndex must not sort its caller's slice out from under it: the same
// slice is the ingress's site list in the reconcile.
func TestRenderIndexDoesNotMutateInput(t *testing.T) {
	page := samplePage()
	before := slices.Clone(page.Views)
	RenderIndex(page)
	if !slices.Equal(before, page.Views) {
		t.Error("RenderIndex reordered the caller's slice")
	}
}

func TestRenderIndexListsEveryViewInHTTPViewsOrder(t *testing.T) {
	out := string(RenderIndex(samplePage()))

	s3 := strings.Index(out, "s3.gfeh.home")
	httpAt := strings.Index(out, "http.gfeh.home")
	ipfs := strings.Index(out, "ipfs.gfeh.home")
	if s3 < 0 || httpAt < 0 || ipfs < 0 {
		t.Fatalf("a view is missing from the rendered page:\n%s", out)
	}
	if s3 >= httpAt || httpAt >= ipfs {
		t.Error("views are not rendered in HTTPViews order")
	}
}

// The port gfeh reports for an HTTP view is a container-side backend port. A
// URL carrying it is refused by every client that tries it, so it must never
// reach the page.
func TestRenderIndexPublishesNoBackendPorts(t *testing.T) {
	out := string(RenderIndex(samplePage()))
	for _, port := range []string{":9000", ":9001", ":9002", ":9003"} {
		if strings.Contains(out, port) {
			t.Errorf("the rendered index carries the container-side port %s", port)
		}
	}
}

// Everything on the page is server-composed, but the partition and TLD are
// still values from configuration, and the page is served unauthenticated.
//
// The assertion is on unescaped *tag openings* rather than on substrings like
// "onerror=". html.EscapeString escapes the angle brackets and the quotes and
// deliberately does not touch "=", so an escaped payload still contains
// "onerror=" as ordinary text — which is inert, and which an assertion on that
// substring reads as a breach. What matters is that no "<" from the input
// survives as markup.
func TestRenderIndexEscapesItsInput(t *testing.T) {
	page := IndexPage{
		Network: `home"><script>alert(1)</script>`,
		TLD:     `home"><script>`,
		Views: []IndexView{
			{View: `<img src=x onerror=alert(1)>`, FQDN: `s3"><script>`, URL: `https://s3.gfeh.home/"><script>`},
		},
	}
	out := string(RenderIndex(page))

	for _, tag := range []string{"<script", "<img", "<iframe"} {
		if strings.Contains(out, tag) {
			t.Errorf("RenderIndex emitted %q as markup:\n%s", tag, out)
		}
	}
	// The payload must still be *present*, escaped -- an assertion that passed
	// because the value was dropped would prove nothing about escaping.
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("the payload was not rendered at all, so nothing was escaped:\n%s", out)
	}
	// And the attribute-breaking quote must not survive inside the href.
	if strings.Contains(out, `href="https://s3.gfeh.home/"><script>"`) {
		t.Errorf("RenderIndex let a quote break out of an attribute:\n%s", out)
	}
}

// A partition that is up but fronting nothing is a real state; the page has to
// say so rather than render blank, which reads as a broken page.
func TestRenderIndexWithNoViews(t *testing.T) {
	out := string(RenderIndex(IndexPage{Network: "office", TLD: "office"}))
	if !strings.Contains(out, "no browsable views") {
		t.Errorf("an empty partition's index does not say so:\n%s", out)
	}
}

// The page is served by Caddy on a box that may have no route to the internet,
// and its whole audience is a browser that has just failed to reach something.
func TestRenderIndexIsSelfContained(t *testing.T) {
	out := string(RenderIndex(samplePage()))
	for _, ref := range []string{"http://", "//cdn", "<script", "<link "} {
		if strings.Contains(out, ref) {
			t.Errorf("the rendered index pulls in %q; it must be self-contained", ref)
		}
	}
}

func TestViewDescriptionCoversEveryHTTPView(t *testing.T) {
	for _, view := range HTTPViews {
		if ViewDescription(view) == "" {
			t.Errorf("view %q has no description", view)
		}
	}
	// An unknown view renders without one rather than with a placeholder.
	if ViewDescription("quic-thing") != "" {
		t.Error("an unknown view was given a description")
	}
}

// Unrecognized views sort after the known ones rather than jumping to the front
// on the Index-returns-minus-one path.
func TestSortIndexViewsPutsUnknownViewsLast(t *testing.T) {
	views := []IndexView{
		{View: "future", FQDN: "future.gfeh.home"},
		{View: ViewIPFS, FQDN: "ipfs.gfeh.home"},
		{View: ViewS3, FQDN: "s3.gfeh.home"},
	}
	SortIndexViews(views)
	want := []string{ViewS3, ViewIPFS, "future"}
	for i, w := range want {
		if views[i].View != w {
			t.Fatalf("SortIndexViews = %v, want order %v", views, want)
		}
	}
}
