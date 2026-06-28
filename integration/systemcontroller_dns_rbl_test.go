// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- RBL / DNSBL config (mock rolodex) ---

func TestDNSRblConfigRoundtrip(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "bl.spamcop.net", Enabled: false},
	}
	if err := c.SetRblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	got, err := c.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if !got.Enabled || len(got.Providers) != 2 || got.Providers[0].Zone != "zen.spamhaus.org" {
		t.Fatalf("unexpected RBL config: %+v", got)
	}
}

func TestDNSDnsblConfigRoundtrip(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	providers := []systemcontroller.RblProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true}}
	if err := c.SetDnsblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	got, err := c.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !got.Enabled || len(got.Providers) != 1 || got.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Fatalf("unexpected DNSBL config: %+v", got)
	}
}

func TestDNSRblConfigInvalidZone(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{{Zone: "not_a_zone", Enabled: true}})
	if err == nil {
		t.Fatal("expected error for invalid zone")
	}
}

func TestDNSRblConfigDuplicateZone(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	err := c.SetRblConfig(ctx, true, []systemcontroller.RblProviderDTO{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "zen.spamhaus.org", Enabled: false},
	})
	if err == nil {
		t.Fatal("expected error for duplicate zone")
	}
}

// --- Local RBL entries (mock rolodex) ---

func TestDNSLocalRblEntries(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddLocalRblEntry(ctx, "ads.example.com", "ad tracker"); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}
	if err := c.AddLocalRblEntry(ctx, "192.0.2.10", "bad ip"); err != nil {
		t.Fatalf("AddLocalRblEntry(ip): %v", err)
	}

	entries, err := c.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if err := c.RemoveLocalRblEntry(ctx, "ads.example.com"); err != nil {
		t.Fatalf("RemoveLocalRblEntry: %v", err)
	}
	entries, err = c.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "192.0.2.10" {
		t.Fatalf("unexpected entries after remove: %+v", entries)
	}
}

func TestDNSLocalRblEntryInvalidName(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if err := c.AddLocalRblEntry(ctx, "not a domain", ""); err == nil {
		t.Fatal("expected error for invalid local RBL entry name")
	}
}

// --- Blocklists catalog + apply (mock rolodex, real local feed server) ---

func TestDNSBlocklistsCatalog(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	resp, err := c.ListBlocklists(ctx)
	if err != nil {
		t.Fatalf("ListBlocklists: %v", err)
	}
	keys := make([]string, 0, len(resp.Feeds))
	for _, f := range resp.Feeds {
		keys = append(keys, f.Key)
	}
	for _, want := range []string{"oisd", "hagezi", "stevenblack", "adguard"} {
		if !slices.Contains(keys, want) {
			t.Errorf("curated catalog missing %q (got %v)", want, keys)
		}
	}
}

func TestDNSBlocklistsApplyAndClear(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	// Serve a small blocklist from a local HTTP server; the systemcontroller's
	// default fetcher performs a real GET against this URL.
	feedBody := "# test feed\n0.0.0.0 ads.example.com\n||tracker.example.net^\nplain.example.org\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feedBody))
	}))
	defer srv.Close()

	feeds, err := c.ApplyBlocklists(ctx, systemcontroller.ApplyBlocklistsRequest{URL: srv.URL, Name: "testfeed"})
	if err != nil {
		t.Fatalf("ApplyBlocklists: %v", err)
	}
	if len(feeds) != 1 || feeds[0] != "testfeed" {
		t.Fatalf("unexpected started feeds: %v", feeds)
	}

	// Poll until the background apply finishes.
	deadline := time.Now().Add(15 * time.Second)
	var done bool
	for time.Now().Before(deadline) {
		status, err := c.ListBlocklists(ctx)
		if err != nil {
			t.Fatalf("ListBlocklists: %v", err)
		}
		if !status.Running {
			for _, s := range status.Status {
				if s.Key == "testfeed" && s.Done {
					done = true
					if s.Error != "" {
						t.Fatalf("apply reported error: %s", s.Error)
					}
					if s.Added != 3 {
						t.Fatalf("expected 3 added, got %d", s.Added)
					}
				}
			}
			if done {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !done {
		t.Fatal("blocklist apply did not finish in time")
	}

	entries, err := c.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 local entries from feed, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Reason != "blocklist:testfeed" {
			t.Errorf("entry %q reason = %q, want blocklist:testfeed", e.Name, e.Reason)
		}
	}

	// Clear all blocklist-sourced entries.
	removed, err := c.ClearBlocklists(ctx, nil)
	if err != nil {
		t.Fatalf("ClearBlocklists: %v", err)
	}
	if removed != 3 {
		t.Fatalf("expected 3 removed, got %d", removed)
	}
	entries, err = c.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", len(entries))
	}
}

func TestDNSBlocklistsApplyUnknownKey(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)
	ctx := context.Background()

	if _, err := c.ApplyBlocklists(ctx, systemcontroller.ApplyBlocklistsRequest{Keys: []string{"does-not-exist"}}); err == nil {
		t.Fatal("expected error for unknown blocklist key")
	}
}
