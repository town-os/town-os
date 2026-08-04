// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/wireguard"
)

// TestSaltIPAMSeedEmptyIsIdentity asserts an unsalted box keeps the exact seed
// it has always used. The overlay subnet derived from this seed is *persisted*
// on the network record, so a changed seed would not migrate anything — it would
// simply hand new networks addresses that disagree with the old ones.
func TestSaltIPAMSeedEmptyIsIdentity(t *testing.T) {
	t.Parallel()
	const seed = "deadbeefdeadbeefdeadbeefdeadbeef"
	if got := saltIPAMSeed("", seed); got != seed {
		t.Errorf("empty salt should pass the seed through, got %q", got)
	}
}

// TestSaltIPAMSeedProducesDistinctSubnets asserts the salt reaches the actual
// subnet, not just the seed string. Two boxes sharing a network namespace share
// a routing table, so two identical /24 overlays are a route conflict even when
// their interfaces are named differently.
//
// /etc/machine-id is generated per container boot and so usually differs
// already — but "usually" is doing too much work when the fallback path is
// os.Hostname(), which is not distinct between two containers at all.
func TestSaltIPAMSeedProducesDistinctSubnets(t *testing.T) {
	t.Parallel()
	const machineID = "deadbeefdeadbeefdeadbeefdeadbeef"

	plain, err := wireguard.SubnetForNetwork(saltIPAMSeed("", machineID), "office")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}
	testBox, err := wireguard.SubnetForNetwork(saltIPAMSeed("test-abc12345", machineID), "office")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}
	devBox, err := wireguard.SubnetForNetwork(saltIPAMSeed("dev-abc12345", machineID), "office")
	if err != nil {
		t.Fatalf("SubnetForNetwork: %v", err)
	}

	if testBox == devBox {
		t.Errorf("test and dev derive the same overlay subnet %s from one machine-id", testBox)
	}
	if testBox == plain && devBox == plain {
		t.Errorf("salting had no effect on the subnet (all %s)", plain)
	}
	// Whatever the salt, the subnet must stay inside the documented 10.64.0.0/10
	// range — that range is chosen to avoid the 10.0/10.1 blocks consumer
	// routers hand out, and a salt must not push an overlay back into them.
	for _, p := range []struct {
		name   string
		subnet netip.Prefix
	}{{"unsalted", plain}, {"test", testBox}, {"dev", devBox}} {
		if second := p.subnet.Addr().As4()[1]; second < 64 || second > 127 {
			t.Errorf("%s subnet %s falls outside 10.64.0.0/10", p.name, p.subnet)
		}
	}
}

// TestNetworkIPAMSeedUsesTheConfiguredSalt asserts the wiring: networkIPAMSeed
// must consult the package-level salt, not just compute the raw seed. A helper
// that is correct but never called is the failure mode this catches.
func TestNetworkIPAMSeedUsesTheConfiguredSalt(t *testing.T) {
	// Not parallel: mutates package-level state that other tests read.
	dir := t.TempDir()
	idPath := filepath.Join(dir, "machine-id")
	if err := os.WriteFile(idPath, []byte("deadbeefdeadbeefdeadbeefdeadbeef\n"), 0600); err != nil {
		t.Fatalf("write machine-id: %v", err)
	}

	origPath, origSalt := machineIDPath, wireGuardSalt
	t.Cleanup(func() { machineIDPath, wireGuardSalt = origPath, origSalt })
	machineIDPath = idPath

	wireGuardSalt = ""
	unsalted := networkIPAMSeed()
	if unsalted != "deadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf("unsalted seed = %q, want the bare machine-id", unsalted)
	}

	wireGuardSalt = "test-abc12345"
	salted := networkIPAMSeed()
	if salted == unsalted {
		t.Errorf("networkIPAMSeed ignored the salt (still %q)", salted)
	}
}
