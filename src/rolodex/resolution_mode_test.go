// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

// TestValidResolutionMode locks the accepted set. An invalid mode must never
// reach rolodex: it is now sent over gRPC, where the server refuses it outright
// rather than warning and falling back, so a value that gets past this check
// turns into a failed RPC and a box left in whatever mode it was already in.
func TestValidResolutionMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{ResolutionModeAuto, ResolutionModeRecursive, ResolutionModeForward} {
		if !ValidResolutionMode(mode) {
			t.Errorf("ValidResolutionMode(%q) = false, want true", mode)
		}
	}
	for _, mode := range []string{"", "iterative", "AUTO ", "forwarding", "recursive "} {
		if ValidResolutionMode(mode) {
			t.Errorf("ValidResolutionMode(%q) = true, want false", mode)
		}
	}
}

// TestResolutionModeDefaultsToAuto pins the default. Auto is the only mode with
// a fallback chain; bare recursive SERVFAILs every external name on a network
// that filters outbound :53, so a box that lost its stored setting must land on
// auto rather than on rolodex's compiled-in choice changing underneath us.
func TestResolutionModeDefaultsToAuto(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	if got := m.ResolutionMode(); got != DefaultResolutionMode {
		t.Errorf("ResolutionMode() = %q, want %q", got, DefaultResolutionMode)
	}
	if DefaultResolutionMode != ResolutionModeAuto {
		t.Errorf("DefaultResolutionMode = %q, want auto", DefaultResolutionMode)
	}
}

// TestSetResolutionModeIsWhatGetsProgrammed asserts the manager holds what an
// operator chose, because that value — not a file — is what the programming
// path pushes to rolodex at boot and after every restart.
func TestSetResolutionModeIsWhatGetsProgrammed(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	m.SetResolutionMode(ResolutionModeForward)
	if got := m.ResolutionMode(); got != ResolutionModeForward {
		t.Errorf("ResolutionMode() = %q after SetResolutionMode(forward)", got)
	}
	m.SetResolutionMode(ResolutionModeRecursive)
	if got := m.ResolutionMode(); got != ResolutionModeRecursive {
		t.Errorf("ResolutionMode() = %q after SetResolutionMode(recursive)", got)
	}
}

// TestResolutionModeIsSafeAgainstAConcurrentForwarderResolution is a race
// regression, and it needs -race to mean anything (make test-race).
//
// The mode used to be read only where rolodex.yml is rendered, so
// SetResolutionMode wrote it with no lock held. It is on a much busier path now:
// forwarders() consults it to decide whether `auto` should discover, and that
// runs on every Forwarders() call — every settings write, and every
// ProgramRolodex pass on the reconcile goroutine. An operator changing the mode
// while the reprogramming tick resolves the list is two goroutines on one field.
func TestResolutionModeIsSafeAgainstAConcurrentForwarderResolution(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{
		ResolvConfPaths: []string{writeResolvConf(t, "resolv.conf", "nameserver 192.168.4.1\n")},
		RouteTablePath:  writeRouteTable(t, ""),
		ForwarderProbe:  acceptAll,
	})

	ctx := t.Context()
	modes := []string{ResolutionModeAuto, ResolutionModeForward, ResolutionModeRecursive}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.SetResolutionMode(modes[i%len(modes)])
		}()
		go func() {
			defer wg.Done()
			// The value is whatever the interleaving produced; that it is
			// read at all without tripping the detector is the assertion.
			_ = m.Forwarders(ctx)
			_ = m.ResolutionMode()
		}()
	}
	wg.Wait()
}

// TestConfiguredResolutionModeSurvivesConstruction covers the boot path, where
// the stored setting arrives through Config rather than a setter.
func TestConfiguredResolutionModeSurvivesConstruction(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{ResolutionMode: ResolutionModeRecursive})
	if got := m.ResolutionMode(); got != ResolutionModeRecursive {
		t.Errorf("ResolutionMode() = %q, want recursive", got)
	}
}

// TestUnitNameIsTheRolodexSystemService keeps the unit name derived from the
// service key rather than spelled out at call sites: the reprogramming path and
// every status read name the same unit, and a test harness runs with its own
// key so it cannot restart the production rolodex.
func TestUnitNameIsTheRolodexSystemService(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{})
	if got, want := m.UnitName(), systemd.SystemServiceUnitName("rolodex"); got != want {
		t.Errorf("UnitName() = %q, want %q", got, want)
	}

	custom := NewManager(Config{Key: "rolodex-test-1"})
	if got, want := custom.UnitName(), systemd.SystemServiceUnitName("rolodex-test-1"); got != want {
		t.Errorf("UnitName() with custom key = %q, want %q", got, want)
	}
}
