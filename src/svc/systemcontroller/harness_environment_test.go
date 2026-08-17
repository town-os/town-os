// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The test and dev harnesses configure the systemcontroller entirely through
// `podman run -e` on the container, and the container's PID 1 is systemd. A
// variable therefore reaches the process only if the unit's PassEnvironment
// allowlist names it, and nothing anywhere fails when it does not: the
// container has the variable, `podman exec` (so every integration test binary)
// sees it, and the one process it was set for silently uses its production
// default instead.
//
// Both omissions this allowlist has carried were that shape.
//
// TOWN_OS_IMAGE_REFRESH is the one that cost a test run. The harness loads
// every image the box needs out of the image cache and sets the variable to 0
// so the controller does not turn around and re-pull the floating ones; blocked
// here, boot re-pulled quay.io/town/rolodex:rc.latest-<arch>, Prometheus and
// node-exporter from quay.io inside parallelEnsureImages, while `wait_for_url`
// gave /status/ping 120 seconds to answer. The suite died at "Waiting for
// systemcontroller API to be ready" with nothing after it, and whether it died
// at all depended on how fast the machine's uplink was that evening.
//
// TOWN_OS_TAG is the one that cost nothing yet: `make dev` sets it and `make
// help` advertises it as "Tag every sibling image derives from", and blocked
// here it did nothing at all.
//
// So the rule is derived rather than remembered: every variable the harness
// sets on a container that runs this unit must appear on the line.
const (
	controllerUnitFile = "integration/testdata/town-os-systemcontroller.service"
	testScript         = "make/test.sh"
	devScript          = "make/dev.sh"
	libScript          = "make/lib.sh"
)

// controllerImageVars are the image variables naming a container that runs the
// systemcontroller unit. A `podman run` line mentioning one is the harness
// starting a box; every other container in these scripts (the registry, gitea,
// the UI test runner, the preflight nginx) runs something else entirely and its
// environment has nothing to do with this allowlist.
var controllerImageVars = []string{"PODMAN_TEST_IMAGE", "PODMAN_DEV_IMAGE"}

// joinContinuations collapses backslash-continued shell lines into one logical
// line each, so a `podman run` spread over twenty lines can be matched as the
// single command it is.
func joinContinuations(script string) []string {
	var lines []string
	var current strings.Builder
	for line := range strings.SplitSeq(script, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if head, continued := strings.CutSuffix(trimmed, "\\"); continued {
			current.WriteString(head)
			current.WriteString(" ")
			continue
		}
		current.WriteString(trimmed)
		lines = append(lines, current.String())
		current.Reset()
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

// envFlagRE matches a `-e VAR=` / `-e "VAR=` podman flag and captures the name.
var envFlagRE = regexp.MustCompile(`-e\s+"?([A-Z][A-Z0-9_]*)=`)

// systemPortPairRE matches one entry of system_port_env's variable:file list in
// make/lib.sh, which is where the per-run port overrides are named.
var systemPortPairRE = regexp.MustCompile(`"([A-Z][A-Z0-9_]*):\.[a-z-]+"`)

// controllerRunLines returns the logical `podman run` commands in script that
// start a container running the systemcontroller unit.
func controllerRunLines(t *testing.T, script string) []string {
	t.Helper()
	var runs []string
	for _, line := range joinContinuations(script) {
		if !strings.Contains(line, "podman run") {
			continue
		}
		for _, image := range controllerImageVars {
			if strings.Contains(line, "${"+image+"}") {
				runs = append(runs, line)
				break
			}
		}
	}
	if len(runs) == 0 {
		t.Fatal("found no podman run for a systemcontroller container; the parser, not the scripts, is what changed")
	}
	return runs
}

// systemPortEnvVars returns the variables system_port_env injects, read from
// make/lib.sh rather than listed here: a port override added there is exactly
// the drift this test exists to catch, and a copy of the list would have to be
// updated by the same commit that forgets the unit file.
func systemPortEnvVars(t *testing.T) []string {
	t.Helper()
	_, body, found := strings.Cut(readRepoFile(t, libScript), "system_port_env() {")
	if !found {
		t.Fatalf("%s has no system_port_env function", libScript)
	}
	body, _, closed := strings.Cut(body, "\n}")
	if !closed {
		t.Fatalf("%s: system_port_env is not terminated", libScript)
	}
	var vars []string
	for _, m := range systemPortPairRE.FindAllStringSubmatch(body, -1) {
		vars = append(vars, m[1])
	}
	if len(vars) == 0 {
		t.Fatalf("%s: system_port_env names no port variables; the parser, not the script, is what changed", libScript)
	}
	return vars
}

// harnessEnvVars returns every variable the harness sets on a systemcontroller
// container, across both scripts.
func harnessEnvVars(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var vars []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			vars = append(vars, name)
		}
	}
	for _, script := range []string{testScript, devScript} {
		for _, run := range controllerRunLines(t, readRepoFile(t, script)) {
			for _, m := range envFlagRE.FindAllStringSubmatch(run, -1) {
				add(m[1])
			}
			// The port overrides arrive as an expanded array rather than
			// as literal -e flags, so they are read from their source.
			if strings.Contains(run, "SYSTEM_PORT_ENV") {
				for _, name := range systemPortEnvVars(t) {
					add(name)
				}
			}
		}
	}
	sort.Strings(vars)
	return vars
}

// passEnvironment returns the names on the unit's PassEnvironment line.
func passEnvironment(t *testing.T) []string {
	t.Helper()
	for line := range strings.SplitSeq(readRepoFile(t, controllerUnitFile), "\n") {
		if after, found := strings.CutPrefix(strings.TrimSpace(line), "PassEnvironment="); found {
			return strings.Fields(after)
		}
	}
	t.Fatalf("%s has no PassEnvironment line", controllerUnitFile)
	return nil
}

// TestHarnessEnvironmentReachesTheController asserts the unit's allowlist names
// every variable the harness sets on the container. A missing name is invisible
// at every layer that could report it, so it is compared statically here.
func TestHarnessEnvironmentReachesTheController(t *testing.T) {
	t.Parallel()

	allowed := passEnvironment(t)
	for _, name := range harnessEnvVars(t) {
		if !slices.Contains(allowed, name) {
			t.Errorf("%s sets %s on the systemcontroller container, but PassEnvironment in %s does not name it: the controller will never see it",
				"make/test.sh or make/dev.sh", name, controllerUnitFile)
		}
	}
}

// TestHarnessDisablesTheImageRefresh asserts both harnesses keep the floating-
// tag refresh switched off.
//
// Separate from the allowlist test above because the two fail for opposite
// reasons: that one derives its requirement from the `-e` flags, so deleting
// the flag would satisfy it. The flag itself is the thing that matters. Every
// image these containers run is placed in them deliberately — loaded from the
// image cache, or built locally under a per-instance tag — precisely because
// the registry is not something a test may depend on. A refresh would reach
// quay.io on every boot to replace an image that was put there on purpose, and
// the readiness wait that follows would be racing the download.
func TestHarnessDisablesTheImageRefresh(t *testing.T) {
	t.Parallel()

	for _, script := range []string{testScript, devScript} {
		for _, run := range controllerRunLines(t, readRepoFile(t, script)) {
			if !strings.Contains(run, EnvImageRefresh+"=0") {
				t.Errorf("%s starts a systemcontroller container without %s=0; boot would re-pull the floating tags it was just handed", script, EnvImageRefresh)
			}
		}
	}
}
