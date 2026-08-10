// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// make/dev.sh's ensure_wireguard_module loads the host kernel module the dev
// box's WireGuard overlays need. Loading a kernel module is host mutation, so a
// test may not actually perform it -- the same rule that keeps the DNS
// redirection in make/dns.sh untestable end-to-end.
//
// So nothing real is reached here. SUDO is pinned to an absolute-path recorder
// that logs its argv and never executes it, exactly as devDNSHarness does, and
// the sysfs path the function probes is redirected into a temp dir. What is
// asserted is what the script WOULD have run, which is the whole interesting
// surface: whether it escalates at all, and whether a failure to load is fatal.
//
// The SUDO override MUST come after sourcing dev.sh: dev.sh sources lib.sh,
// which assigns SUDO="sudo HOME=$HOME" unconditionally, so setting it in a
// prelude would be overwritten by the real thing and a bug here would run
// modprobe on the developer's box.
type devWGHarness struct {
	t        *testing.T
	root     string
	stateDir string
	stubDir  string
	binDir   string
	sysfs    string
}

func newDevWGHarness(t *testing.T) *devWGHarness {
	t.Helper()

	h := &devWGHarness{
		t:        t,
		root:     repoRoot(t),
		stateDir: t.TempDir(),
		stubDir:  t.TempDir(),
		binDir:   t.TempDir(),
	}
	// Deliberately inside a temp dir and NOT created: the default state is
	// "module absent", and a test opts into presence by creating it.
	h.sysfs = filepath.Join(t.TempDir(), "wireguard")

	// The privileged recorder: logs argv, never executes it. Exits non-zero
	// only when a marker file says to, which is how the load-failure branch is
	// driven. Leading VAR=VAL words are dropped so the log holds the real
	// command, since production SUDO carries a HOME=$HOME assignment.
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
if [ -f "${STUB_STATE}/fail" ]; then exit 1; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(h.binDir, "record-privileged"), []byte(script), 0o755); err != nil {
		t.Fatalf("write recorder: %v", err)
	}
	return h
}

// moduleLoaded makes the sysfs probe report the module as already present.
func (h *devWGHarness) moduleLoaded() {
	h.t.Helper()
	if err := os.MkdirAll(h.sysfs, 0o755); err != nil {
		h.t.Fatalf("create sysfs dir: %v", err)
	}
}

// modprobeFails makes the recorder report a failed load.
func (h *devWGHarness) modprobeFails() {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.stubDir, "fail"), nil, 0o600); err != nil {
		h.t.Fatalf("write fail marker: %v", err)
	}
}

// run sources make/dev.sh (whose sourcing guard stops it before dispatch),
// overrides SUDO, and calls ensure_wireguard_module. It returns the combined
// output and fails the test if the function returns non-zero -- the function is
// documented as non-fatal, so a non-zero return is itself the bug.
func (h *devWGHarness) run() string {
	h.t.Helper()

	prelude := ". make/dev.sh\n" +
		"SUDO='" + filepath.Join(h.binDir, "record-privileged") + " HOME=/nonexistent'\n"

	cmd := exec.CommandContext(h.t.Context(), "bash", "-c", prelude+"ensure_wireguard_module\n")
	cmd.Dir = h.root
	cmd.Env = append(os.Environ(),
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STATE_DIR="+h.stateDir,
		"STUB_STATE="+h.stubDir,
		"WG_MODULE_SYSFS="+h.sysfs,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("ensure_wireguard_module returned non-zero (it must be non-fatal): %v\n%s", err, out)
	}
	return string(out)
}

// privilegedLog returns the recorded privileged calls, in order.
func (h *devWGHarness) privilegedLog() []string {
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

// TestEnsureWireGuardModuleSkipsWhenPresent pins the no-escalation path. A
// present module covers both "already loaded" and "built into the kernel", and
// neither needs anything done -- so `make dev` must not prompt for a password
// on the overwhelmingly common case.
func TestEnsureWireGuardModuleSkipsWhenPresent(t *testing.T) {
	t.Parallel()

	h := newDevWGHarness(t)
	h.moduleLoaded()
	h.run()

	if calls := h.privilegedLog(); len(calls) != 0 {
		t.Errorf("module already present must not escalate, but ran: %v", calls)
	}
}

// TestEnsureWireGuardModuleLoadsWhenAbsent pins the actual load. The exact
// argv matters: this is the command that makes network creation work at all,
// and it is invoked through ${SUDO} rather than by naming sudo directly.
func TestEnsureWireGuardModuleLoadsWhenAbsent(t *testing.T) {
	t.Parallel()

	h := newDevWGHarness(t)
	h.run()

	calls := h.privilegedLog()
	if len(calls) != 1 || calls[0] != "modprobe wireguard" {
		t.Errorf("expected exactly one privileged call %q, got %v", "modprobe wireguard", calls)
	}
}

// TestEnsureWireGuardModuleLoadFailureIsNonFatal pins the degradation. A dev
// box without WireGuard is still a working dev box for everything that is not
// an overlay network, so a failed load must warn rather than abort `make dev`
// -- and the warning must name the consequence, since a silent degrade is how
// this class of bug stays unnoticed.
func TestEnsureWireGuardModuleLoadFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	h := newDevWGHarness(t)
	h.modprobeFails()
	out := h.run() // run() already fails the test on a non-zero return.

	if !strings.Contains(out, "wireguard") {
		t.Errorf("a failed load must warn about the wireguard module, got:\n%s", out)
	}
	if !strings.Contains(out, "network") {
		t.Errorf("the warning must name what breaks (network creation), got:\n%s", out)
	}
}
