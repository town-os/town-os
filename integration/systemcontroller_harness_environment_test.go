// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// controllerUnit is the unit the harness runs the systemcontroller as, and
// controllerUnitPath is where the test image installs it.
const (
	controllerUnit     = "town-os-systemcontroller.service"
	controllerUnitPath = "/etc/systemd/system/" + controllerUnit
)

// mutableManagerEnv are the variables the harness rewrites in the systemd
// manager environment partway through a run: make/test.sh runs `systemctl
// set-environment DEBUG=1 LOG_LEVEL=debug` before the UI phase. The running
// controller may predate or postdate that call, so its value for these is not
// required to match the container's — everything else is.
var mutableManagerEnv = map[string]bool{"LOG_LEVEL": true, "DEBUG": true}

// controllerMainPID returns the PID of the running systemcontroller, or 0 when
// systemd reports none.
func controllerMainPID(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "show", "-p", "MainPID", "--value", controllerUnit).Output()
	if err != nil {
		t.Skipf("systemctl show %s: %v (not running under the harness)", controllerUnit, err)
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" || pid == "0" {
		t.Skipf("%s has no main PID; nothing to read an environment from", controllerUnit)
	}
	return pid
}

// processEnv reads a running process's environment out of /proc.
func processEnv(t *testing.T, pid string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + pid + "/environ")
	if err != nil {
		t.Skipf("read /proc/%s/environ: %v", pid, err)
	}
	env := map[string]string{}
	for entry := range bytes.SplitSeq(raw, []byte{0}) {
		name, value, found := strings.Cut(string(entry), "=")
		if found {
			env[name] = value
		}
	}
	return env
}

// passEnvironmentNames returns the allowlist from the installed unit file.
func passEnvironmentNames(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(controllerUnitPath)
	if err != nil {
		t.Skipf("read %s: %v (not running under the harness)", controllerUnitPath, err)
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		if after, found := strings.CutPrefix(strings.TrimSpace(line), "PassEnvironment="); found {
			return strings.Fields(after)
		}
	}
	t.Fatalf("%s has no PassEnvironment line", controllerUnitPath)
	return nil
}

// The harness configures the controller entirely through `podman run -e`, and
// systemd's PassEnvironment is an allowlist standing between those variables
// and the process. A name missing from it fails nowhere: the container has the
// variable, this test binary — started by `podman exec` — sees it, and only the
// controller does not.
//
// So this reads the environment of the actual running controller out of /proc
// and compares it against the container's own. Statically the same contract is
// checked in src/svc/systemcontroller (TestHarnessEnvironmentReachesThe
// Controller); this is the half that would have caught a unit file that was
// correct in the repository and stale in the image.
func TestHarnessEnvironmentReachesTheRunningController(t *testing.T) {
	t.Parallel()

	env := processEnv(t, controllerMainPID(t))
	for _, name := range passEnvironmentNames(t) {
		if mutableManagerEnv[name] {
			continue
		}
		want, set := os.LookupEnv(name)
		if !set {
			// Not every allowlisted variable is set by every harness —
			// dev sets TOWN_OS_TAG and no port overrides, test the
			// reverse. Nothing to compare.
			continue
		}
		got, reached := env[name]
		if !reached {
			t.Errorf("%s is set on the container but never reached the controller; PassEnvironment in %s does not carry it", name, controllerUnitPath)
			continue
		}
		if got != want {
			t.Errorf("%s = %q in the controller, %q on the container", name, got, want)
		}
	}
}

// The specific one that cost a run: with the refresh left on, boot re-pulls
// every floating tag — rolodex, Prometheus, node-exporter — from quay.io while
// the harness gives /status/ping 120 seconds to answer, and replaces images
// that were deliberately loaded from the image cache with whatever the registry
// holds now. Asserted on the running process rather than on the make script,
// because the make script was never the half that was wrong.
func TestImageRefreshIsOffInsideTheController(t *testing.T) {
	t.Parallel()

	want, set := os.LookupEnv(systemcontroller.EnvImageRefresh)
	if !set {
		t.Skipf("%s is not set on this container; not running under the harness", systemcontroller.EnvImageRefresh)
	}

	env := processEnv(t, controllerMainPID(t))
	got, reached := env[systemcontroller.EnvImageRefresh]
	if !reached {
		t.Fatalf("%s=%q is set on the container but not on the controller: every boot re-pulls the floating tags the harness just loaded into it", systemcontroller.EnvImageRefresh, want)
	}
	if got != want {
		t.Fatalf("%s = %q in the controller, %q on the container", systemcontroller.EnvImageRefresh, got, want)
	}
}
