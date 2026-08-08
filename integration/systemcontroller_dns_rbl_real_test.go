// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"errors"
	"net"
	"slices"
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

// TestRolodexDnsblAllowlistRoundtripReal verifies the allowlist RPCs against a
// real rolodex container. It is also the version-floor check: the RPCs landed in
// rolodex v0.4.3, and an older daemon fails this with Unimplemented rather than
// silently doing nothing.
func TestRolodexDnsblAllowlistRoundtripReal(t *testing.T) {
	t.Parallel()
	client, _ := initRolodexRealTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const name = "cdn.vendor.example"

	// Rolodex normalizes an allowlist name into fully-qualified form on the way
	// in and hands it back that way, so a bare name goes in and "cdn.vendor
	// .example." comes out. That asymmetry is why the handler runs the listed
	// names back through trimDNSRoot — the local blocklist beside it stores
	// names verbatim, and the two tables must not disagree about the same name.
	const stored = name + "."

	if err := client.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{
		Name:   name,
		Reason: "false positive",
	}); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}

	entries, err := client.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries: %v", err)
	}
	var got *upstream.DnsblAllowlistEntry
	for _, e := range entries {
		if e.Name == stored {
			got = e
			break
		}
	}
	if got == nil {
		t.Fatalf("expected %q in allowlist entries, got %+v", stored, entries)
	}
	if got.Reason != "false positive" {
		t.Fatalf("expected the reason to round-trip, got %q", got.Reason)
	}

	// Removal takes the bare name the operator would have been shown; rolodex
	// normalizes it the same way, so the entry it stored still matches.
	if err := client.RemoveDnsblAllowlistEntry(ctx, name); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}
	entries, err = client.ListDnsblAllowlistEntries(ctx)
	if err != nil {
		t.Fatalf("ListDnsblAllowlistEntries after remove: %v", err)
	}
	for _, e := range entries {
		if e.Name == stored {
			t.Fatalf("entry %q still present after remove", stored)
		}
	}
}

// TestRolodexDnsblAllowlistOverridesLocalRblReal is the behavioral proof, and the
// only one that shows the feature is worth anything: a name blocked by a local
// RBL entry resolves again once an allowlist entry covers it, and goes back to
// NXDOMAIN when that entry is withdrawn.
//
// The allowlist entry is placed on the *parent* domain deliberately — an entry
// covers the name and every name beneath it, which is what makes it usable
// against a feed that listed a whole vendor.
//
// It forwards to a local stub upstream rather than the internet, so it is
// hermetic: a captive network must not turn "the allowlist did not work" and
// "the box has no upstream DNS" into the same failure.
func TestRolodexDnsblAllowlistOverridesLocalRblReal(t *testing.T) {
	t.Parallel()

	upstreamAddr := startStubDNS(t)
	client, dnsPort := initRolodexRealTestForwarders(t, []string{upstreamAddr})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		parent  = "vendor.example"
		blocked = "tracker.vendor.example"
	)

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", rolodex.DNSLoopback+":"+dnsPort)
		},
	}

	// resolves reports whether the stub's answer comes back through rolodex.
	resolves := func() bool {
		lookupCtx, lc := context.WithTimeout(ctx, 5*time.Second)
		defer lc()
		addrs, err := resolver.LookupHost(lookupCtx, blocked+".")
		return err == nil && slices.Contains(addrs, "192.0.2.55")
	}

	// nxdomain reports whether rolodex answers with an authoritative NXDOMAIN.
	nxdomain := func() bool {
		lookupCtx, lc := context.WithTimeout(ctx, 5*time.Second)
		defer lc()
		_, err := resolver.LookupHost(lookupCtx, blocked+".")
		var dnsErr *net.DNSError
		return errors.As(err, &dnsErr) && dnsErr.IsNotFound
	}

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	// Baseline: the name forwards to the stub and resolves.
	waitFor("the name to resolve through the stub upstream", resolves)

	// Blocked by a local RBL entry.
	if err := client.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: blocked, Reason: "test"}); err != nil {
		t.Fatalf("AddLocalRblEntry: %v", err)
	}
	waitFor("the blocked name to return NXDOMAIN", nxdomain)

	// An allowlist entry on the parent beats the local block, and covers the
	// subdomain without naming it.
	if err := client.AddDnsblAllowlistEntry(ctx, &upstream.DnsblAllowlistEntry{
		Name:   parent,
		Reason: "false positive",
	}); err != nil {
		t.Fatalf("AddDnsblAllowlistEntry: %v", err)
	}
	waitFor("the allowlisted name to resolve again", resolves)

	// Withdrawing the exemption restores the block; the local RBL entry was
	// never removed, only overridden.
	if err := client.RemoveDnsblAllowlistEntry(ctx, parent); err != nil {
		t.Fatalf("RemoveDnsblAllowlistEntry: %v", err)
	}
	waitFor("the block to return once the allowlist entry is withdrawn", nxdomain)
}
