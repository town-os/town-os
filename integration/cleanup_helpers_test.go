// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// Teardown here is deliberately unconditional: every t.Cleanup stops, disables
// and removes every unit the package could have produced, and deletes every
// subvolume the test could have created, without first checking whether any of
// them are still there. That is what makes it a safety net -- it has to mop up
// after a mid-test t.Fatal or panic, where the test's own teardown never ran.
//
// The cost is that the ordinary path, where the code under test already removed
// everything, reports a failure for each step. A passing run printed forty-odd
// lines of "Unit ... not loaded", "Could not statfs: No such file or directory"
// and "remove /etc/systemd/system/...: no such file or directory", none of which
// means anything went wrong. Noise that always appears is noise nobody reads,
// and a real leak -- a subvolume that would not delete because something still
// has it mounted, which breaks the next concurrent run -- looks exactly like the
// rest of it.
//
// So the target being gone is a successful teardown and prints nothing. Anything
// else still prints, because anything else is a leak.

// alreadyGone reports whether err is a teardown step saying its target was
// already removed. The strings come from three sources with no common error
// type between them: systemd's dbus replies, btrfs-progs on stderr, and the
// kernel via os.Remove.
func alreadyGone(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, absent := range []string{
		"no such file or directory", // os.Remove of a unit file already deleted
		"could not statfs",          // btrfs subvolume delete of a subvolume already deleted
		"not loaded",                // systemd stop of a unit it never loaded
		"does not exist",            // systemd disable of a unit file already removed
		"no such container",         // podman stop/rm after the unit already took it down
	} {
		if strings.Contains(msg, absent) {
			return true
		}
	}
	return false
}

// logCleanupf reports a teardown failure worth reading. A step whose target was
// already gone prints nothing; everything else prints, because a teardown that
// fails for any other reason has left something behind for the next run.
func logCleanupf(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if alreadyGone(err) {
		return
	}
	t.Logf("cleanup "+format+": %v", append(append([]any{}, args...), err)...)
}

// The classifier is the whole safety of this: a needle that is too broad stops
// reporting a leak that would break the next concurrent run, which is the one
// thing this suite cannot tolerate. The second half of the table is therefore
// the load-bearing half.
func TestAlreadyGoneClassifiesTeardownErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, true},
		{"fs.ErrNotExist", fs.ErrNotExist, true},
		{"wrapped fs.ErrNotExist", fmt.Errorf("uninstall unit: %w", fs.ErrNotExist), true},
		{"unit not loaded", errors.New("Unit town-os-package--core-redis-7.0.service not loaded."), true},
		{"unit file missing", errors.New("Unit file town-os-package--core-redis-7.0-tcp.socket does not exist."), true},
		{"remove missing unit file", errors.New("remove /etc/systemd/system/x.service: no such file or directory"), true},
		{"btrfs statfs", errors.New("btrfs subvolume delete: exit status 1\nERROR: Could not statfs: No such file or directory"), true},
		{"podman missing container", errors.New("Error: no such container town-os-package--core-redis-7.0"), true},

		// Every one of these is a leak. None may be swallowed.
		{"subvolume busy", errors.New("btrfs subvolume delete: exit status 1\nERROR: Could not destroy subvolume: Device or resource busy"), false},
		{"permission denied", errors.New("remove /etc/systemd/system/x.service: permission denied"), false},
		{"dbus timeout", errors.New("connection timed out"), false},
		{"unit failed to stop", errors.New("Job for x.service failed"), false},
		{"read-only filesystem", errors.New("remove /etc/systemd/system/x.service: read-only file system"), false},
		{"podman still running", errors.New("Error: container town-os-x is running"), false},
	} {
		if got := alreadyGone(tc.err); got != tc.want {
			t.Errorf("alreadyGone(%s) = %v, want %v: %v", tc.name, got, tc.want, tc.err)
		}
	}
}
