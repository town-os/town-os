package systemcontroller

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
)

func TestParseBlocklistMixedFormats(t *testing.T) {
	// A single feed mixing hosts-file, plain-domain, and Adblock syntax, plus
	// comments, blanks, duplicates, and unsupported rules.
	feed := strings.Join([]string{
		"# comment line",
		"! adblock comment",
		"[Adblock Plus 2.0]",
		"",
		"0.0.0.0 ads.example.com",
		"127.0.0.1 tracker.example.net",
		"plain-domain.example.org",
		"||adblock.example.com^",
		"||with-modifier.example.com^$third-party",
		"@@||allowlisted.example.com^", // exception — skip
		"||*.wildcard.example.com^",    // wildcard — skip
		"##.cosmetic",                  // cosmetic — skip
		"0.0.0.0 ads.example.com",      // duplicate — dedup
		"0.0.0.0 0.0.0.0",             // sink->IP — skip (candidate is IP)
		"localhost",                    // skip
		"   ads.example.com   ",        // duplicate after trim
		"good.example.io # inline comment",
	}, "\n")

	got, err := parseBlocklist(strings.NewReader(feed))
	if err != nil {
		t.Fatalf("parseBlocklist: %v", err)
	}

	want := []string{
		"ads.example.com",
		"tracker.example.net",
		"plain-domain.example.org",
		"adblock.example.com",
		"with-modifier.example.com",
		"good.example.io",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parseBlocklist mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestNormalizeBlocklistDomain(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Example.COM", "example.com", true},
		{"trailing.example.com.", "trailing.example.com", true},
		{"sub.domain.example.org", "sub.domain.example.org", true},
		{"", "", false},
		{"localhost", "", false},
		{"192.0.2.1", "", false},
		{"single", "", false},
		{"*.wild.com", "", false},
		{"has space.com", "", false},
		{"a^b.com", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeBlocklistDomain(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("normalizeBlocklistDomain(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveApplyFeeds(t *testing.T) {
	// Empty request => all curated feeds.
	all, err := resolveApplyFeeds(ApplyBlocklistsRequest{})
	if err != nil {
		t.Fatalf("resolveApplyFeeds(empty): %v", err)
	}
	if len(all) != len(curatedBlocklists) {
		t.Fatalf("expected %d curated feeds, got %d", len(curatedBlocklists), len(all))
	}

	// Specific keys.
	sel, err := resolveApplyFeeds(ApplyBlocklistsRequest{Keys: []string{"oisd", "stevenblack"}})
	if err != nil {
		t.Fatalf("resolveApplyFeeds(keys): %v", err)
	}
	if len(sel) != 2 || sel[0].Key != "oisd" || sel[1].Key != "stevenblack" {
		t.Fatalf("unexpected selection: %+v", sel)
	}

	// Unknown key.
	if _, err := resolveApplyFeeds(ApplyBlocklistsRequest{Keys: []string{"nope"}}); err == nil {
		t.Fatal("expected error for unknown key")
	}

	// Custom URL.
	custom, err := resolveApplyFeeds(ApplyBlocklistsRequest{URL: "http://localhost:1234/list.txt", Name: "myfeed"})
	if err != nil {
		t.Fatalf("resolveApplyFeeds(custom): %v", err)
	}
	if len(custom) != 1 || custom[0].Key != "myfeed" || custom[0].URL != "http://localhost:1234/list.txt" {
		t.Fatalf("unexpected custom feed: %+v", custom)
	}

	// Custom URL defaults the key to "custom" when no name is given.
	def, err := resolveApplyFeeds(ApplyBlocklistsRequest{URL: "https://example.com/l"})
	if err != nil {
		t.Fatalf("resolveApplyFeeds(custom default): %v", err)
	}
	if def[0].Key != "custom" {
		t.Fatalf("expected default key 'custom', got %q", def[0].Key)
	}

	// Invalid URL scheme.
	if _, err := resolveApplyFeeds(ApplyBlocklistsRequest{URL: "ftp://example.com/l"}); err == nil {
		t.Fatal("expected error for invalid URL scheme")
	}

	// Invalid custom name.
	if _, err := resolveApplyFeeds(ApplyBlocklistsRequest{URL: "https://example.com/l", Name: "bad name"}); err == nil {
		t.Fatal("expected error for invalid feed name")
	}
}

func TestApplyBlocklistFeed(t *testing.T) {
	rc := &rolodex.MockClient{}
	feed := BlocklistFeed{Key: "test", Name: "Test", URL: "http://example/list.txt"}

	fetch := func(_ context.Context, url string) (io.ReadCloser, error) {
		if url != feed.URL {
			t.Errorf("fetch got url %q, want %q", url, feed.URL)
		}
		body := "0.0.0.0 a.example.com\n||b.example.com^\nc.example.com\n# comment\n"
		return io.NopCloser(strings.NewReader(body)), nil
	}

	added, err := applyBlocklistFeed(context.Background(), rc, fetch, feed, nil)
	if err != nil {
		t.Fatalf("applyBlocklistFeed: %v", err)
	}
	if added != 3 {
		t.Fatalf("added = %d, want 3", added)
	}

	entries, err := rc.ListLocalRblEntries(context.Background())
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 local entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Reason != blocklistReasonPrefix+"test" {
			t.Errorf("entry %q reason = %q, want %q", e.Name, e.Reason, blocklistReasonPrefix+"test")
		}
	}
}

func TestApplyBlocklistFeedFetchError(t *testing.T) {
	rc := &rolodex.MockClient{}
	feed := BlocklistFeed{Key: "test", URL: "http://example/list.txt"}
	fetch := func(_ context.Context, _ string) (io.ReadCloser, error) {
		return nil, errors.New("boom")
	}
	if _, err := applyBlocklistFeed(context.Background(), rc, fetch, feed, nil); err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestApplyBlocklistFeedContextCancelled(t *testing.T) {
	rc := &rolodex.MockClient{}
	feed := BlocklistFeed{Key: "test", URL: "http://example/list.txt"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before any add
	fetch := func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("a.example.com\nb.example.com\n")), nil
	}
	added, err := applyBlocklistFeed(ctx, rc, fetch, feed, nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if added != 0 {
		t.Fatalf("added = %d, want 0 after immediate cancel", added)
	}
}
