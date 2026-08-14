// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

// binfmtMiscDir is the kernel's binfmt_misc interface. It is GLOBAL state — not
// namespaced away from a container in the host's user namespace — which is the
// entire reason this file exists.
const binfmtMiscDir = "/proc/sys/fs/binfmt_misc"

// TestBinfmtMiscIsNotReachableFromThisContainer guards a leak that cost a
// developer a `make deps` run after every test run for weeks.
//
// This suite's container is --privileged in the host's user namespace, so its
// /proc/sys/fs/binfmt_misc IS the machine's. Both of Debian's binfmt paths were
// live in here and both wrote straight through: systemd-binfmt, whose ExecStop
// is `--unregister` and clears EVERY handler rather than only its own, and
// binfmt-support's update-binfmts, which mounts binfmt_misc itself when it is
// not already mounted.
//
// The observed damage on an x86_64 Manjaro host: all 35 qemu-* handlers gone,
// replaced by a single entry —
//
//	/proc/sys/fs/binfmt_misc/python3.11 -> interpreter /usr/bin/python3.11
//
// on a host running Python 3.14 with no /usr/bin/python3.11 and none of
// Debian's binfmt tooling. The registration came from in here and pointed at an
// interpreter that exists only in this rootfs. The cost was that
// `TARGET=aarch64 make push-rc` stopped working after any test run, because the
// qemu-aarch64 handler a cross build needs went with the rest.
//
// Containerfile.systemd masks all four units. This asserts the result rather
// than the masks: what matters is that nothing in here can reach the interface,
// however it might have tried to get there.
func TestBinfmtMiscIsNotReachableFromThisContainer(t *testing.T) {
	t.Parallel()

	// The write interface. Its absence means binfmt_misc was never mounted in
	// this namespace, so there is nothing for any tool — masked unit or not — to
	// register into.
	register := filepath.Join(binfmtMiscDir, "register")
	if _, err := os.Stat(register); err == nil {
		t.Errorf("%s exists: binfmt_misc is mounted in this container, and anything "+
			"in here that registers a handler writes it into the HOST's kernel table", register)
	} else if !os.IsNotExist(err) {
		// A permission error is fine and is not the failure under test: it still
		// means nothing here is writing handlers to the host.
		t.Logf("stat %s: %v (not mounted, or not readable — either is acceptable)", register, err)
	}

	// Named individually so a failure says which mask was dropped rather than
	// only that something regressed. A mask is a symlink to /dev/null.
	for _, unit := range []string{
		"systemd-binfmt.service",
		"binfmt-support.service",
		"proc-sys-fs-binfmt_misc.automount",
		"proc-sys-fs-binfmt_misc.mount",
	} {
		path := filepath.Join("/etc/systemd/system", unit)
		target, err := os.Readlink(path)
		if err != nil {
			t.Errorf("%s is not masked (%v); see the mask line in "+
				"integration/testdata/Containerfile.systemd", unit, err)
			continue
		}
		if target != "/dev/null" {
			t.Errorf("%s links to %q, not /dev/null, so it is not masked", unit, target)
		}
	}
}
