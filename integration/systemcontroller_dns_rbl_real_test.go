// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
)

// TestRolodexRblConfigRoundtripReal verifies SetRblConfig/GetRblConfig against a
// real rolodex container.
func TestRolodexRblConfigRoundtripReal(t *testing.T) {
	t.Parallel()
	client, _ := initRolodexRealTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	providers := []*upstream.RblConfig{
		{Zone: "zen.spamhaus.org", Enabled: true},
		{Zone: "bl.spamcop.net", Enabled: false},
	}
	if err := client.SetRblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetRblConfig: %v", err)
	}

	status, err := client.GetRblConfig(ctx)
	if err != nil {
		t.Fatalf("GetRblConfig: %v", err)
	}
	if !status.Enabled {
		t.Fatal("expected RBL enabled")
	}
	if len(status.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d: %+v", len(status.Providers), status.Providers)
	}
}

// TestRolodexDnsblConfigRoundtripReal verifies SetDnsblConfig/GetDnsblConfig
// against a real rolodex container.
func TestRolodexDnsblConfigRoundtripReal(t *testing.T) {
	t.Parallel()
	client, _ := initRolodexRealTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	providers := []*upstream.DnsblConfig{{Zone: "dbl.spamhaus.org", Enabled: true}}
	if err := client.SetDnsblConfig(ctx, true, providers); err != nil {
		t.Fatalf("SetDnsblConfig: %v", err)
	}

	status, err := client.GetDnsblConfig(ctx)
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !status.Enabled || len(status.Providers) != 1 || status.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Fatalf("unexpected DNSBL status: %+v", status)
	}
}

// TestRolodexLocalRblBlocksForwardReal proves a local RBL entry (a domain name)
// blocks a forward A lookup with NXDOMAIN on a real rolodex container, and that
// removing it restores resolution behavior. This is the mechanism behind the
// curated blocklists (OISD/HaGeZi/StevenBlack/AdGuard) being loaded as local
// entries.
func TestRolodexLocalRblBlocksForwardReal(t *testing.T) {
	t.Parallel()
	client, dnsPort := initRolodexRealTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const blocked = "blocked-rbl-test.example.com"

	if err := client.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: blocked, Reason: "blocklist:test"}); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}

	entries, err := client.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name == blocked {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in local RBL entries, got %+v", blocked, entries)
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", rolodex.DNSLoopback+":"+dnsPort)
		},
	}

	// Poll until the blocked domain returns NXDOMAIN (IsNotFound).
	deadline := time.Now().Add(20 * time.Second)
	var blockedOK bool
	for time.Now().Before(deadline) {
		lookupCtx, lc := context.WithTimeout(ctx, 5*time.Second)
		_, lookupErr := resolver.LookupHost(lookupCtx, blocked+".")
		lc()
		var dnsErr *net.DNSError
		if errors.As(lookupErr, &dnsErr) && dnsErr.IsNotFound {
			blockedOK = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !blockedOK {
		t.Fatalf("expected NXDOMAIN for blocked domain %q", blocked)
	}

	// Removing the entry succeeds.
	if err := client.RemoveLocalRblEntry(ctx, blocked); err != nil {
		t.Fatalf("RemoveLocalRblEntry: %v", err)
	}
	entries, err = client.ListLocalRblEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalRblEntries after remove: %v", err)
	}
	for _, e := range entries {
		if e.Name == blocked {
			t.Fatalf("entry %q still present after remove", blocked)
		}
	}
}
