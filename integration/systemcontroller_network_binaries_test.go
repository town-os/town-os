// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"os"
	"os/exec"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

// TestNetworkUnitBinariesPresent asserts that the rootfs running these tests
// ships every binary the generated per-network WireGuard unit needs.
//
// This is the one check that catches a missing wg-quick as a *failure* rather
// than as silence. Everywhere else the symptom is indistinguishable from a
// network that was simply never started: systemd cannot exec the binary, the
// RestartUnit job returns "failed", applyNetworkTransport turns that into an
// error, and POST /networks/create answers 500 having already committed the
// network row — leaving a network that exists with no transport behind it.
// TestNetworkReconcileRealSystemd cannot see it, because it deliberately seeds
// a *disabled* network (to avoid creating a real interface in the shared host
// netns) and so only ever asserts LoadState, which is "loaded" whether or not
// the ExecStart binary exists.
//
// The container this runs in is the same rootfs that runs the units, because
// the systemcontroller runs as a service under the container's own PID 1 — see
// integration/testdata/Containerfile.systemd and Containerfile.dev. On a real
// box the units run on the host, which `make deps` provisions.
//
// No network is created and no interface is touched, so this is safe to run
// concurrently with anything.
func TestNetworkUnitBinariesPresent(t *testing.T) {
	t.Parallel()

	for _, path := range systemd.NetworkUnitExecPaths() {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("the generated network unit execs %s, but this image does not ship it: %v\n"+
				"Install wireguard-tools in the image (see integration/testdata/Containerfile.systemd).", path, err)
			continue
		}
		if info.IsDir() || info.Mode()&0111 == 0 {
			t.Errorf("%s exists but is not executable (mode %v)", path, info.Mode())
		}
	}

	// wg-quick resolves `ip` through PATH for every link, address, and route
	// operation, so iproute2 is as load-bearing as wg-quick itself even though
	// it never appears in the unit text. Checked by lookup rather than by a
	// fixed path, since the two distros disagree on where it lives.
	if _, err := exec.LookPath("ip"); err != nil {
		t.Errorf("wg-quick needs `ip` from iproute2 and it is not on PATH: %v\n"+
			"Install iproute2 in the image (see integration/testdata/Containerfile.systemd).", err)
	}
}
