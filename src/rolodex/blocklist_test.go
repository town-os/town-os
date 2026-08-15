// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"testing"
)

// TestSetBlocklistCopiesCallerSlices guards the manager against a caller that
// keeps mutating the slice it handed over. The list is read by the programming
// path on another goroutine (the reprogramming tick), so a shared backing array
// is a data race and, worse, a way for a half-built list to be pushed to
// rolodex.
func TestSetBlocklistCopiesCallerSlices(t *testing.T) {
	t.Parallel()

	providers := []BlocklistProvider{{Zone: "dbl.spamhaus.org", Enabled: true}}
	dnsbl := Blocklist{Enabled: true, Providers: providers}

	m := NewManager(Config{})
	m.SetBlocklist(dnsbl)

	// Mutate everything the caller still holds.
	providers[0].Zone = "mutated.example.com"
	providers[0].Enabled = false

	got := m.Blocklist()
	if len(got.Providers) != 1 || got.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Errorf("providers followed the caller's mutation: %+v", got.Providers)
	}
	if !got.Providers[0].Enabled {
		t.Error("provider enabled flag followed the caller's mutation")
	}
}

// TestBlocklistReturnsCopies is the same guarantee in the other direction: a
// reader that edits what it got back must not reach into the manager.
func TestBlocklistReturnsCopies(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	m.SetBlocklist(Blocklist{
		Enabled:   true,
		Providers: []BlocklistProvider{{Zone: "dbl.spamhaus.org", Enabled: true}},
	})

	got := m.Blocklist()
	got.Providers[0].Zone = "mutated.example.com"

	again := m.Blocklist()
	if again.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Errorf("a reader's mutation reached the manager: %q", again.Providers[0].Zone)
	}
}

// TestZeroBlocklistIsDisabledWithNoProviders pins the zero value, which is what
// a box that has never configured a blocklist programs into rolodex. It must
// mean "disabled, nothing configured" rather than "enabled with an empty list",
// which would turn every lookup into a no-op with the cost of a lookup.
func TestZeroBlocklistIsDisabledWithNoProviders(t *testing.T) {
	t.Parallel()

	var zero Blocklist
	if zero.Enabled {
		t.Error("zero Blocklist is enabled")
	}
	if len(zero.Providers) != 0 {
		t.Errorf("zero Blocklist has %d providers", len(zero.Providers))
	}
	if zero.RefusalCooldownSecs != 0 {
		t.Errorf("zero Blocklist cooldown = %d, want 0 (meaning rolodex's default)", zero.RefusalCooldownSecs)
	}
}
