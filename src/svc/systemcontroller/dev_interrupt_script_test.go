// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Ctrl-C is the documented way to stop `make dev`: the target ends in the UI
// dev server running in the foreground, and interrupting it is how an operator
// gets their shell back. make/dev.sh's INT/HUP/TERM trap is what turns that
// signal into an orderly stop, and it used to `exit 130` -- which made every
// stop print `make: *** [make/include.mk:211: dev] Error 130`, reporting a
// failure for the normal path, and skipped the teardown below it entirely, so
// the dev container and its monitoring containers were left running with the
// host's ports still held.
//
// These tests pin both halves of that: the interrupt is a success (exit 0, no
// failure for make to print) and it tears the box down, while a genuine failure
// still surfaces as one.
//
// Nothing real is reached. SUDO is pinned to an absolute-path recorder that
// logs its argv and never executes it -- the same construction as
// devDNSHarness, and for the same reason: the alternative, shadowing `sudo` on
// PATH, would rewrite the DNS of whatever machine runs the suite if the shadow
// were ever missed. restore_host_dns is stubbed to a marker for the same
// reason, since a test may not touch the host's resolver at all.
//
// The trap lines are not retyped here -- they are lifted verbatim out of
// make/dev.sh and eval'd (see devTrapLines). A copy in the test would keep
// passing while dev.sh regressed, which is precisely the bug this covers.
//
// There is deliberately no integration counterpart, on the same grounds as
// dev_dns_script_test.go: an integration test runs inside the test container,
// and a faithful exercise of `make dev` redirects the host's resolver, loads a
// host kernel module, and binds host ports -- all forbidden to tests outright.
// The stubbed unit pass below is the complete coverage.
type devInterruptHarness struct {
	t        *testing.T
	root     string
	stateDir string
	stubDir  string
	binDir   string
}

func newDevInterruptHarness(t *testing.T) *devInterruptHarness {
	t.Helper()

	h := &devInterruptHarness{
		t:        t,
		root:     repoRoot(t),
		stateDir: t.TempDir(),
		stubDir:  t.TempDir(),
		binDir:   t.TempDir(),
	}

	// The privileged recorder: logs argv, never executes it. Leading VAR=VAL
	// words are dropped so the log holds the real command, since production
	// SUDO carries a HOME=$HOME assignment.
	script := `#!/usr/bin/env bash
args=()
seen=0
for a in "$@"; do
  if [ "${seen}" -eq 0 ] && [ "${a#*=}" != "${a}" ] && [ "${a#-}" = "${a}" ]; then
    continue
  fi
  seen=1
  args+=("${a}")
done
printf '%s\n' "${args[*]}" >> "${STUB_STATE}/privileged.log"
exit 0
`
	if err := os.WriteFile(filepath.Join(h.binDir, "record-privileged"), []byte(script), 0o755); err != nil {
		t.Fatalf("write recorder: %v", err)
	}
	return h
}

// devTrapLines returns the trap statements from make/dev.sh's `start` arm,
// verbatim. Lifting them out of the script is the whole point: these tests
// drive the traps the target actually installs, so editing them in dev.sh
// re-aims the tests rather than silently leaving them behind.
func (h *devInterruptHarness) devTrapLines() string {
	h.t.Helper()

	raw, err := os.ReadFile(filepath.Join(h.root, "make", "dev.sh"))
	if err != nil {
		h.t.Fatalf("read make/dev.sh: %v", err)
	}
	var lines []string
	for l := range strings.SplitSeq(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "trap ") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	if len(lines) != 2 {
		h.t.Fatalf("expected exactly the EXIT and INT/HUP/TERM traps in make/dev.sh, got %v", lines)
	}
	return strings.Join(lines, "\n") + "\n"
}

// run sources make/dev.sh (whose sourcing guard stops it before dispatch),
// pins SUDO to the recorder, stubs the two host-mutating calls, installs
// dev.sh's own traps, and then evaluates body. It returns the combined output
// and the exit status, and does NOT fail on a non-zero one -- the status is
// what several of these tests are about.
func (h *devInterruptHarness) run(body string) (string, int) {
	h.t.Helper()

	// The SUDO override MUST come after sourcing dev.sh: dev.sh sources lib.sh,
	// which assigns SUDO unconditionally, so an earlier assignment would be
	// overwritten by the real thing.
	prelude := ". make/dev.sh\n" +
		"SUDO='" + filepath.Join(h.binDir, "record-privileged") + " HOME=/nonexistent'\n" +
		"PODMAN_DEV_CONTAINER=town-os-dev\n" +
		"step() { :; }\nsubstep() { :; }\nwarn() { :; }\n" +
		// A test may not touch the host resolver, so the restore is a marker
		// rather than the real thing. Ordering still gets asserted: it writes
		// into the same log the privileged recorder appends to.
		"restore_host_dns() { printf 'restore-host-dns\\n' >> \"${STUB_STATE}/privileged.log\"; }\n" +
		h.devTrapLines()

	cmd := exec.CommandContext(h.t.Context(), "bash", "-c", prelude+body)
	cmd.Dir = h.root
	cmd.Env = append(os.Environ(),
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STATE_DIR="+h.stateDir,
		"STUB_STATE="+h.stubDir,
	)
	// Its own process group, so the self-delivered SIGINT below reaches only
	// this shell and its children. Without it the signal would land on the
	// group running the test binary.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		h.t.Fatalf("bash: %v\n%s", err, out)
	}
	return string(out), exitErr.ExitCode()
}

// calls returns the recorded privileged calls and restore markers, in order.
func (h *devInterruptHarness) calls() []string {
	h.t.Helper()

	raw, err := os.ReadFile(filepath.Join(h.stubDir, "privileged.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		h.t.Fatalf("read privileged.log: %v", err)
	}
	var out []string
	for l := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// interruptBody mimics `cd ui && bun run dev -- --host` being Ctrl-C'd: the
// shell blocks in a foreground child while the signal arrives for the whole
// process group, which is what a terminal does and what bash's trap dispatch
// is sensitive to. `kill -INT 0` targets the group; the harness put this shell
// in its own, so nothing outside it is signalled.
//
// The foreground sleep is the dev server, and it is bounded rather than
// indefinite so a regression that loses the trap fails in seconds instead of
// hanging until the test binary's timeout.
const interruptBody = "( sleep 0.2; kill -INT 0 ) &\nsleep 10\n"

// TestDevInterruptExitsZero is the reason this file exists. Ctrl-C is the
// documented way to stop dev, so it must not hand make a non-zero status to
// report as a build failure.
func TestDevInterruptExitsZero(t *testing.T) {
	t.Parallel()

	h := newDevInterruptHarness(t)
	out, code := h.run(interruptBody)

	if code != 0 {
		t.Errorf("Ctrl-C on the dev server must exit 0 (make prints a failure otherwise), got %d\n%s", code, out)
	}
}

// TestDevInterruptTearsDown pins the other half of the old bug: `exit 130`
// jumped straight over the teardown, leaving the dev container and the
// monitoring containers that share the host's network namespace running with
// its ports held.
func TestDevInterruptTearsDown(t *testing.T) {
	t.Parallel()

	h := newDevInterruptHarness(t)
	out, _ := h.run(interruptBody)

	calls := h.calls()
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "restore-host-dns") {
		t.Errorf("the interrupt must restore the host's resolver, got %v\n%s", calls, out)
	}
	if !strings.Contains(joined, "podman rm -f town-os-dev") {
		t.Errorf("the interrupt must remove the dev container, got %v\n%s", calls, out)
	}
}

// TestDevTeardownOrder pins stop-before-remove. The monitoring containers share
// the dev container's network and PID namespaces, so removing it first orphans
// conmon processes that keep holding the host's ports -- which is why the next
// `make dev` starts by force-removing them.
func TestDevTeardownOrder(t *testing.T) {
	t.Parallel()

	h := newDevInterruptHarness(t)
	out, code := h.run("dev_teardown\n")
	if code != 0 {
		t.Fatalf("dev_teardown must not fail (it runs from a trap): exit %d\n%s", code, out)
	}

	// FIRST occurrence of each, not the last: this harness installs dev.sh's
	// real traps, so the EXIT trap fires on the way out and appends a second
	// restore-host-dns after the teardown. That trailing entry is correct
	// behaviour -- restore_host_dns is a documented no-op with nothing to
	// restore -- but taking the last index would read it as the restore
	// happening after the container was already gone.
	calls := h.calls()
	restore, stop, remove := -1, -1, -1
	record := func(at *int, i int) {
		if *at < 0 {
			*at = i
		}
	}
	for i, c := range calls {
		switch {
		case c == "restore-host-dns":
			record(&restore, i)
		case strings.Contains(c, "systemctl stop"):
			record(&stop, i)
		case strings.Contains(c, "podman rm -f"):
			record(&remove, i)
		}
	}
	if restore < 0 || stop < 0 || remove < 0 {
		t.Fatalf("dev_teardown must restore DNS, stop the services, and remove the container; got %v", calls)
	}
	// DNS first: the container half is slow and needs no resolver of ours.
	if restore >= stop || stop >= remove {
		t.Errorf("expected restore(%d) < stop(%d) < remove(%d) in %v", restore, stop, remove, calls)
	}
}

// TestDevTeardownStopsMonitoringServices pins the unit list. A service missing
// here is a container left holding a host port after dev stops, and the next
// `make dev` fails to bind it.
func TestDevTeardownStopsMonitoringServices(t *testing.T) {
	t.Parallel()

	h := newDevInterruptHarness(t)
	h.run("dev_teardown\n")

	joined := strings.Join(h.calls(), "\n")
	for _, unit := range []string{
		"town-os-system--node-exporter.service",
		"town-os-system--prometheus.service",
		"town-os-system--prometheus-network.service",
		"town-os-system--monitoring-ui.service",
		"town-os-system--monitoring-ui-network.service",
	} {
		if !strings.Contains(joined, unit) {
			t.Errorf("dev_teardown must stop %s, got:\n%s", unit, joined)
		}
	}
}

// TestDevFailureKeepsContainer pins the deliberate asymmetry between the two
// traps. An interrupt is an operator stopping dev and takes the box down; a
// failure -- bun_install dying, the dev server refusing to bind -- is not, and
// must leave the container up so it can still be inspected with
// `make dev-logs`, with the real status reaching make.
func TestDevFailureKeepsContainer(t *testing.T) {
	t.Parallel()

	h := newDevInterruptHarness(t)
	out, code := h.run("exit 7\n")

	if code != 7 {
		t.Errorf("a genuine failure must reach make unchanged, got exit %d\n%s", code, out)
	}
	calls := h.calls()
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "restore-host-dns") {
		t.Errorf("a failure must still restore the host's resolver, got %v", calls)
	}
	if strings.Contains(joined, "podman rm -f") {
		t.Errorf("a failure must leave the dev container up for inspection, got %v", calls)
	}
}
