// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"strings"
	"testing"
)

// A readiness wait that fails silently is worse than no wait at all, because
// the run still stops — it just stops without saying anything. The whole of a
// real failure was:
//
//	==> Restarting systemcontroller after image loading
//	==> Waiting for systemcontroller API to be ready
//	make[2]: *** [make/include.mk:125: test-integration-build] Error 1
//
// `wait_for_url` returns 1 and `set -e` ends the script; the trap then removes
// the container, and the journal that held the reason goes with it. The cause
// (TOWN_OS_IMAGE_REFRESH never reaching the controller, so every boot re-pulled
// the floating tags over the network while the 120s wait counted down) had to
// be found by reading source instead of output.
//
// require_controller_ready in make/lib.sh is the fix: same wait, and on timeout
// it prints the last ping body, the unit's status and the tail of its journal
// before exiting. This asserts the readiness waits keep using it.
func TestControllerReadinessWaitsReportWhyTheyFailed(t *testing.T) {
	t.Parallel()

	const helper = "require_controller_ready"
	if !strings.Contains(readRepoFile(t, libScript), helper+"() {") {
		t.Fatalf("%s no longer defines %s", libScript, helper)
	}

	for _, script := range []string{testScript, devScript} {
		body := readRepoFile(t, script)
		for _, line := range joinContinuations(body) {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if !strings.Contains(trimmed, "wait_for_url") || strings.Contains(trimmed, helper) {
				continue
			}
			if strings.Contains(trimmed, "/status/ping") {
				t.Errorf("%s waits on the systemcontroller with a bare wait_for_url: %q\n"+
					"a timeout there ends the run with no output at all; use %s", script, trimmed, helper)
			}
		}
	}
}
