// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

// enabledExposure is a published file in the shape gfehd reports one.
func enabledExposure(token, path, filename string) Exposure {
	e := Exposure{Token: token, Path: path, Enabled: true}
	if filename != "" {
		e.Filename = ptr(filename)
	}
	return e
}

// A disabled exposure is not served, so a row for it would be a link that 404s
// — which reads as a broken index rather than a withdrawn file.
func TestPublicFilesFromExposuresDropsWhatIsNotServed(t *testing.T) {
	files := PublicFilesFromExposures([]Exposure{
		enabledExposure("live", "/reports/q3.pdf", "q3.pdf"),
		{Token: "withdrawn", Path: "/secret.txt", Enabled: false},
		{Token: "", Path: "/nameless", Enabled: true},
	})

	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if files[0].Token != "live" {
		t.Errorf("kept the wrong exposure: %+v", files[0])
	}
}

// Withdrawing is the only thing that takes a file off this page, so it has to
// actually take it off.
func TestPublicFilesFromExposuresWithdrawalRemovesTheRow(t *testing.T) {
	e := enabledExposure("tok", "/a/b.txt", "b.txt")
	if got := PublicFilesFromExposures([]Exposure{e}); len(got) != 1 {
		t.Fatalf("an enabled exposure is not listed: %+v", got)
	}
	e.Enabled = false
	if got := PublicFilesFromExposures([]Exposure{e}); len(got) != 0 {
		t.Errorf("a withdrawn exposure is still listed: %+v", got)
	}
}

// filename, then the last element of the path, then the token. A row has to say
// something, and the token is a poor name but the truth about what the link is.
func TestPublicFileNameFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Exposure
		want string
	}{
		{"filename wins", enabledExposure("t1", "/reports/q3-final.pdf", "Q3.pdf"), "Q3.pdf"},
		{"path basename next", enabledExposure("t2", "/reports/q3-final.pdf", ""), "q3-final.pdf"},
		{"blank filename is no filename", Exposure{Token: "t3", Path: "/a/b.txt", Filename: ptr("  "), Enabled: true}, "b.txt"},
		{"token last", enabledExposure("t4", "", ""), "t4"},
		{"a path of only a separator is not a name", enabledExposure("t5", "/", ""), "t5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := PublicFilesFromExposures([]Exposure{tc.in})
			if len(files) != 1 {
				t.Fatalf("got %d files, want 1", len(files))
			}
			if files[0].Name != tc.want {
				t.Errorf("Name = %q, want %q", files[0].Name, tc.want)
			}
		})
	}
}

// The page is compared against what is on disk to decide whether to write, so an
// order that varied between reconciles would rewrite the file every pass.
func TestPublicFilesAreOrderedByNameThenToken(t *testing.T) {
	files := PublicFilesFromExposures([]Exposure{
		enabledExposure("zzz", "", "beta.txt"),
		enabledExposure("bbb", "", "alpha.txt"),
		enabledExposure("aaa", "", "beta.txt"),
	})

	var got []string
	for _, f := range files {
		got = append(got, f.Name+"/"+f.Token)
	}
	want := []string{"alpha.txt/bbb", "beta.txt/aaa", "beta.txt/zzz"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestRenderPublicIndexIsDeterministic(t *testing.T) {
	page := PublicIndexPage{
		Network: "home",
		FQDN:    "http.gfeh.home",
		Files: []PublicFile{
			{Name: "b.txt", Token: "t2"},
			{Name: "a.txt", Token: "t1"},
		},
	}
	first := string(RenderPublicIndex(page))
	for range 5 {
		if got := string(RenderPublicIndex(page)); got != first {
			t.Fatalf("render differs between calls:\n%s\n---\n%s", first, got)
		}
	}
}

// The caller's slice is theirs; the renderer sorts a copy.
func TestRenderPublicIndexDoesNotMutateInput(t *testing.T) {
	files := []PublicFile{{Name: "b.txt", Token: "t2"}, {Name: "a.txt", Token: "t1"}}
	RenderPublicIndex(PublicIndexPage{Network: "home", Files: files})

	if files[0].Name != "b.txt" {
		t.Errorf("the input slice was reordered: %+v", files)
	}
}

// Every row is a working link, which is the whole point: a directory of
// published files that omitted the links would be a list of names nobody could
// open.
func TestRenderPublicIndexLinksEveryFile(t *testing.T) {
	out := string(RenderPublicIndex(PublicIndexPage{
		Network: "home",
		FQDN:    "http.gfeh.home",
		Files:   []PublicFile{{Name: "q3.pdf", Token: "abc123"}, {Name: "photo.jpg", Token: "def456"}},
	}))

	for _, want := range []string{`href="/f/abc123"`, `href="/f/def456"`, "q3.pdf", "photo.jpg"} {
		if !strings.Contains(out, want) {
			t.Errorf("the index is missing %q:\n%s", want, out)
		}
	}
}

// The links are root-relative: the page is served under exactly the name they
// resolve under, so an absolute URL would be one more chance to name a host the
// ingress does not route — and this package does not know the TLD anyway.
func TestRenderPublicIndexLinksAreRelative(t *testing.T) {
	out := string(RenderPublicIndex(PublicIndexPage{
		Network: "home",
		FQDN:    "http.gfeh.home",
		Files:   []PublicFile{{Name: "q3.pdf", Token: "abc123"}},
	}))

	if strings.Contains(out, "https://http.gfeh.home") {
		t.Errorf("a link is absolute:\n%s", out)
	}
	// And never the container-side backend port, which is reachable from
	// nowhere a reader sits.
	if strings.Contains(out, ":9001") {
		t.Errorf("the index carries the container-side port:\n%s", out)
	}
}

// A filename is somebody's, and a token comes off a wire. Neither may
// restructure the page.
func TestRenderPublicIndexEscapesItsInput(t *testing.T) {
	out := string(RenderPublicIndex(PublicIndexPage{
		Network: `home"><script>alert(1)</script>`,
		FQDN:    "http.gfeh.home",
		Files: []PublicFile{{
			Name:  `<img src=x onerror=alert(1)>`,
			Token: `a" onclick="alert(1)`,
		}},
	}))

	for _, bad := range []string{"<script>", "<img src=x", `onclick="alert`} {
		if strings.Contains(out, bad) {
			t.Errorf("unescaped %q reached the page:\n%s", bad, out)
		}
	}
}

// An empty partition says so. A blank page reads as a rendering failure, which
// is the class of thing this index exists to stop.
func TestRenderPublicIndexWithNoFiles(t *testing.T) {
	out := string(RenderPublicIndex(PublicIndexPage{Network: "home", FQDN: "http.gfeh.home"}))

	if !strings.Contains(out, "Nothing has been published") {
		t.Errorf("an empty index does not say it is empty:\n%s", out)
	}
}

// Served by the pages container, which has no egress a browser would follow.
func TestRenderPublicIndexIsSelfContained(t *testing.T) {
	out := string(RenderPublicIndex(PublicIndexPage{
		Network: "home",
		Files:   []PublicFile{{Name: "a.txt", Token: "t1"}},
	}))

	for _, bad := range []string{"<script", "http://", "https://", "//cdn"} {
		if strings.Contains(out, bad) {
			t.Errorf("the page reaches outside itself via %q:\n%s", bad, out)
		}
	}
}

// A token goes into an href, so it is URL-escaped before it is HTML-escaped.
func TestPublishedPathEscapesTheToken(t *testing.T) {
	if got := PublishedPath("a/b"); got != "/f/a%2Fb" {
		t.Errorf("PublishedPath(%q) = %q, want /f/a%%2Fb", "a/b", got)
	}
	if got := PublishedPath("plain"); got != "/f/plain" {
		t.Errorf("PublishedPath(plain) = %q", got)
	}
}
