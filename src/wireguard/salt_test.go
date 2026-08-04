// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package wireguard

import (
	"strings"
	"testing"
)

// TestUnsaltedDerivationsArePinned is the regression guard for every deployed
// box. These literals are what Town OS has always computed, and they are written
// out rather than recomputed with sha256 so the test cannot agree with a
// refactor that changed the derivation.
//
// An interface name is not just a label: it is the wg-quick config *filename*,
// so changing one orphans the running device and its generated unit. A listen
// port change silently moves the UDP endpoint every enrolled peer is dialing.
// Neither failure is visible until remote access stops working.
func TestUnsaltedDerivationsArePinned(t *testing.T) {
	t.Parallel()
	cases := []struct {
		network string
		iface   string
		port    int
	}{
		{"home", "town4ea1", 52483},
		{"office", "town5cc3", 53064},
		{"lab", "towna511", 53366},
	}
	for _, tc := range cases {
		if got := InterfaceName("", tc.network); got != tc.iface {
			t.Errorf("InterfaceName(\"\", %q) = %q, want %q (this renames a live device)", tc.network, got, tc.iface)
		}
		if got := ListenPortForName("", tc.network); got != tc.port {
			t.Errorf("ListenPortForName(\"\", %q) = %d, want %d (this moves the peer-facing endpoint)", tc.network, got, tc.port)
		}
	}
}

// TestSaltChangesInterfaceName asserts the salt actually separates two
// instances. Without this the whole mechanism is decorative: a test box and a
// dev box would keep deriving the same device name in the shared netns.
func TestSaltChangesInterfaceName(t *testing.T) {
	t.Parallel()
	plain := InterfaceName("", "office")
	testBox := InterfaceName("test-abc12345", "office")
	devBox := InterfaceName("dev-abc12345", "office")

	if testBox == plain {
		t.Errorf("salted name %q matches the unsalted name", testBox)
	}
	if devBox == plain {
		t.Errorf("salted name %q matches the unsalted name", devBox)
	}
	// The test-vs-dev case is the one that motivated this: same checkout, same
	// network name, both --net host.
	if testBox == devBox {
		t.Errorf("test and dev derive the same interface %q in one checkout", testBox)
	}
}

// TestSaltChangesListenPort asserts the UDP port moves with the salt too. The
// interface name alone is not enough: two devices with different names still
// cannot both bind the same port in one namespace.
func TestSaltChangesListenPort(t *testing.T) {
	t.Parallel()
	plain := ListenPortForName("", "office")
	testBox := ListenPortForName("test-abc12345", "office")
	devBox := ListenPortForName("dev-abc12345", "office")

	if testBox == plain || devBox == plain {
		t.Errorf("salted ports (%d, %d) collide with the unsalted port %d", testBox, devBox, plain)
	}
	if testBox == devBox {
		t.Errorf("test and dev derive the same listen port %d in one checkout", testBox)
	}
}

// TestSaltSeparatesCheckouts asserts the INSTANCE_ID half of the salt works:
// two worktrees running test-full concurrently must not collide either.
func TestSaltSeparatesCheckouts(t *testing.T) {
	t.Parallel()
	a := InterfaceName("test-aaaaaaaa", "office")
	b := InterfaceName("test-bbbbbbbb", "office")
	if a == b {
		t.Errorf("two checkouts derive the same interface %q", a)
	}
	if pa, pb := ListenPortForName("test-aaaaaaaa", "office"), ListenPortForName("test-bbbbbbbb", "office"); pa == pb {
		t.Errorf("two checkouts derive the same listen port %d", pa)
	}
}

// TestSaltedInterfaceNameStaysKernelLegal asserts a long salt cannot push the
// name past the kernel's 15-character limit (IFNAMSIZ-1). The salt only widens
// the hash input, so the output is always "town" + 4 hex — but an implementation
// that appended the salt instead would produce a name the kernel rejects at
// device-creation time, long after the config was written.
func TestSaltedInterfaceNameStaysKernelLegal(t *testing.T) {
	t.Parallel()
	for _, salt := range []string{"", "t", "test-abc12345", strings.Repeat("x", 512)} {
		got := InterfaceName(salt, "office")
		if len(got) > 15 {
			t.Errorf("InterfaceName(%q…) = %q (%d chars), exceeds the 15-char kernel limit", salt[:min(len(salt), 8)], got, len(got))
		}
		if !strings.HasPrefix(got, "town") || len(got) != 8 {
			t.Errorf("InterfaceName(salt len %d) = %q, want the 8-char town-prefixed form", len(salt), got)
		}
	}
}

// TestSaltIsStableForTheSameInputs asserts the derivation is a pure function of
// (salt, name). Dev's database survives across `make dev` runs, so a salt that
// produced a different name on each call would strand stored subnets against
// devices named for a previous run.
func TestSaltIsStableForTheSameInputs(t *testing.T) {
	t.Parallel()
	for range 8 {
		if got := InterfaceName("dev-abc12345", "office"); got != InterfaceName("dev-abc12345", "office") {
			t.Fatalf("InterfaceName is not deterministic: got %q", got)
		}
	}
}
