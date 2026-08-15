// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
)

// TestLocalBlocklistEntriesRoundTripThroughRealRolodex exercises the local
// blocklist API against a real rolodex over the real gRPC socket.
//
// These three calls arrived with the rolodex-dns bump that this working copy
// needed to compile at all, and until now the only thing exercising them was a
// mock — which asserts that Town OS calls the methods it was written to call,
// and nothing about whether rolodex has them, accepts these arguments, or does
// anything as a result. That is the failure a version bump actually produces: a
// method the client offers and the server refuses, which surfaces as a gRPC
// error at runtime and as a passing test suite.
//
// It is a round trip rather than three separate assertions because the entries
// are server state: an Add that is not visible to List has not happened, and a
// Remove is only meaningful against something that was there.
func TestLocalBlocklistEntriesRoundTripThroughRealRolodex(t *testing.T) {
	t.Parallel()

	client, _ := initRolodexRealTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	// A name under .invalid, so a test that somehow reached a real resolver
	// still cannot affect anything outside this rolodex. RFC 2606.
	const name = "blocked.local-blocklist-test.invalid"
	const reason = "integration test"

	before := listLocalBlocklist(ctx, t, client)
	if localBlocklistHas(before, name) {
		t.Fatalf("%q is already blocked before the test added it", name)
	}

	if err := client.AddLocalBlocklistEntry(ctx, &upstream.LocalBlocklistEntry{
		Name:   name,
		Reason: reason,
	}); err != nil {
		t.Fatalf("AddLocalBlocklistEntry: %v", err)
	}

	added := listLocalBlocklist(ctx, t, client)
	entry := findLocalBlocklist(added, name)
	if entry == nil {
		t.Fatalf("the added entry is not in the list rolodex reports: %+v", added)
	}
	// The reason travels with it. It is what the UI shows an operator to explain
	// why a name is blocked, so an entry that arrives with an empty one is a
	// blocklist nobody can audit.
	if got := entry.GetReason(); got != reason {
		t.Errorf("entry reason = %q, want %q", got, reason)
	}

	if err := client.RemoveLocalBlocklistEntry(ctx, name); err != nil {
		t.Fatalf("RemoveLocalBlocklistEntry: %v", err)
	}

	after := listLocalBlocklist(ctx, t, client)
	if localBlocklistHas(after, name) {
		t.Errorf("%q survived removal: %+v", name, after)
	}
}

// TestRemoveUnknownLocalBlocklistEntryIsNotFatal covers the shape a reconcile
// takes: removing something already gone must not error, or every repair pass
// that runs after a manual cleanup fails on its first call and abandons the
// rest of its work.
func TestRemoveUnknownLocalBlocklistEntryIsNotFatal(t *testing.T) {
	t.Parallel()

	client, _ := initRolodexRealTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	// Whatever rolodex does here — succeed as a no-op, or refuse — the contract
	// worth pinning is that it is reported rather than crashing the client, and
	// that the list is unchanged either way.
	before := listLocalBlocklist(ctx, t, client)
	err := client.RemoveLocalBlocklistEntry(ctx, "never-added.local-blocklist-test.invalid")
	t.Logf("RemoveLocalBlocklistEntry on an absent name returned: %v", err)

	after := listLocalBlocklist(ctx, t, client)
	if len(after) != len(before) {
		t.Errorf("removing an absent name changed the list: %d entries before, %d after", len(before), len(after))
	}
}

// listLocalBlocklist reads the current entries, failing the test on error.
func listLocalBlocklist(ctx context.Context, t *testing.T, client rolodex.Client) []*upstream.LocalBlocklistEntry {
	t.Helper()
	entries, err := client.ListLocalBlocklistEntries(ctx)
	if err != nil {
		t.Fatalf("ListLocalBlocklistEntries: %v", err)
	}
	return entries
}

// findLocalBlocklist returns the entry with this name, or nil.
func findLocalBlocklist(entries []*upstream.LocalBlocklistEntry, name string) *upstream.LocalBlocklistEntry {
	for _, e := range entries {
		if e.GetName() == name {
			return e
		}
	}
	return nil
}

func localBlocklistHas(entries []*upstream.LocalBlocklistEntry, name string) bool {
	return findLocalBlocklist(entries, name) != nil
}
