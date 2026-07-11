// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
)

// TestValidateDNSResolutionModeRejectsUnknownModes guards the blast radius: an
// unrecognized mode would be rendered straight into rolodex.yml, and rolodex
// refuses to start with a config it cannot parse — taking DNS down for the whole
// box. The validator is the only thing standing between a typo in the settings
// UI and that outage.
func TestValidateDNSResolutionModeRejectsUnknownModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{
		rolodex.ResolutionModeAuto,
		rolodex.ResolutionModeRecursive,
		rolodex.ResolutionModeForward,
	} {
		if err := ValidateDNSResolutionMode(mode); err != nil {
			t.Errorf("ValidateDNSResolutionMode(%q) = %v, want nil", mode, err)
		}
	}

	for _, mode := range []string{"", "iterative", "Recursive", "forwarding", "8.8.8.8"} {
		if err := ValidateDNSResolutionMode(mode); err == nil {
			t.Errorf("ValidateDNSResolutionMode(%q) = nil, want an error", mode)
		}
	}
}

// TestDNSResolutionModeIsWiredIntoSettingsValidators ensures the validator is
// actually reachable from the generic settings API — the map entry, not just the
// function, is what enforces it.
func TestDNSResolutionModeIsWiredIntoSettingsValidators(t *testing.T) {
	t.Parallel()

	v, ok := settingsValidators["dns_resolution_mode"]
	if !ok {
		t.Fatal("dns_resolution_mode has no validator; a bad value would reach rolodex.yml")
	}
	if err := v("iterative"); err == nil {
		t.Fatal("the registered validator accepted an invalid mode")
	}
}

// TestDNSResolutionModeDefaultIsAuto pins the out-of-the-box behavior: Town OS
// tries the root servers first (so queries stay private wherever the network
// permits it) but degrades to encrypted DNS or an upstream resolver rather than
// SERVFAILing where it does not. Bare "recursive" has no fallback at all.
func TestDNSResolutionModeDefaultIsAuto(t *testing.T) {
	t.Parallel()

	got, ok := account.DefaultSettings["dns_resolution_mode"]
	if !ok {
		t.Fatal("dns_resolution_mode is not seeded in DefaultSettings")
	}
	if got != rolodex.ResolutionModeAuto {
		t.Fatalf("default dns_resolution_mode = %q, want %q", got, rolodex.ResolutionModeAuto)
	}
}
