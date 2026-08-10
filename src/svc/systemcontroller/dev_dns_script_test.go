// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// make/dns.sh redirects the host's resolver through the dev rolodex. Every
// function in it shells out to `resolvectl`, `ip`, `systemctl`, or `sudo`, and
// the whole point of the file is mutating host state -- which is exactly what
// the test rules forbid a test from touching: no test may rewrite
// /etc/resolv.conf, signal systemd-resolved, or occupy the host's DNS port.
//
// So these tests never let a real call through. The privileged half is captured
// by overriding lib.sh's SUDO with an absolute path to a recorder that logs its
// argv and exits -- deliberately NOT by shadowing `sudo` on PATH, because a
// PATH lookup that missed the shadow for any reason would run the real thing
// and rewrite the DNS of whatever machine is running the suite. Overriding the
// variable makes that unreachable: no code path names `sudo` at all. Stub
// `ip`/`resolvectl`/`systemctl` serve canned fixtures for the unprivileged
// reads.
//
// What is asserted is what the script WOULD have run. That is the whole
// testable surface, and it is the interesting one: every bug this file has had
// was a wrong argument or a missing call, not a misbehaving resolvectl.
//
// There is deliberately no integration counterpart. An integration test runs
// inside the test container against real systemd and real btrfs, and the only
// faithful exercise of this code is mutating the host resolver of the machine
// running the suite -- forbidden outright, and destructive to a developer's box
// even if it were not. The stubbed unit pass below is the complete coverage.

// devDNSHarness sources make/dns.sh in a sandbox: a temp STATE_DIR for its two
// backup files, and a stub bin dir that shadows every external command it can
// reach.
type devDNSHarness struct {
	t        *testing.T
	root     string
	stateDir string
	stubDir  string
	binDir   string
}

func newDevDNSHarness(t *testing.T) *devDNSHarness {
	t.Helper()

	h := &devDNSHarness{
		t:        t,
		root:     repoRoot(t),
		stateDir: t.TempDir(),
		stubDir:  t.TempDir(),
		binDir:   t.TempDir(),
	}

	// The privileged recorder. It records and returns success, and never
	// executes its argument -- so `${SUDO} resolvectl dns ...` and
	// `${SUDO} cp ... /etc/resolv.conf` are observable without happening. The
	// leading VAR=VAL words are dropped so the log holds the real command:
	// production SUDO carries a `HOME=$HOME` assignment and the harness mirrors
	// that shape.
	h.writeStub("record-privileged", `
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
`)

	// resolvectl: only ever reached un-sudo'd by link_setting, which reads a
	// current value. Serves a per-(verb,link) fixture, empty when unset.
	h.writeStub("resolvectl", `
f="${STUB_STATE}/resolvectl-$1-$2"
if [ -f "${f}" ]; then cat "${f}"; fi
exit 0
`)

	// ip: serves the canned default-route table, keyed by the -4/-6 flag.
	h.writeStub("ip", `
f="${STUB_STATE}/ip$1"
if [ -f "${f}" ]; then cat "${f}"; fi
exit 0
`)

	// systemctl: is-active systemd-resolved. Absent marker means not running.
	h.writeStub("systemctl", `
if [ -f "${STUB_STATE}/resolved-active" ]; then exit 0; fi
exit 1
`)

	return h
}

func (h *devDNSHarness) writeStub(name, body string) {
	h.t.Helper()
	script := "#!/usr/bin/env bash\n" + body
	if err := os.WriteFile(filepath.Join(h.binDir, name), []byte(script), 0o755); err != nil {
		h.t.Fatalf("write stub %s: %v", name, err)
	}
}

// fixture seeds a canned file the stubs read.
func (h *devDNSHarness) fixture(name, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.stubDir, name), []byte(content), 0o600); err != nil {
		h.t.Fatalf("write fixture %s: %v", name, err)
	}
}

// resolvedRunning makes the systemctl stub report systemd-resolved as active.
func (h *devDNSHarness) resolvedRunning() {
	h.t.Helper()
	h.fixture("resolved-active", "")
}

// run sources make/dns.sh and evaluates script against it, returning stdout.
// step/substep/warn are stubbed to silence: they are lib.sh's progress
// printers, and sourcing lib.sh would drag in unrelated setup.
func (h *devDNSHarness) run(script string) string {
	h.t.Helper()

	// SUDO is pinned to the absolute path of the recorder, so nothing in
	// make/dns.sh can reach a real privilege-escalation binary even if PATH is
	// unexpected. The trailing assignment mirrors production's "sudo HOME=$HOME".
	prelude := "SUDO='" + filepath.Join(h.binDir, "record-privileged") + " HOME=/nonexistent'\n" +
		"step() { :; }\nsubstep() { :; }\nwarn() { :; }\n" +
		"_yellow=''\n_reset=''\n" +
		". make/dns.sh\n"

	cmd := exec.CommandContext(h.t.Context(), "bash", "-c", prelude+script)
	cmd.Dir = h.root
	cmd.Env = append(os.Environ(),
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STATE_DIR="+h.stateDir,
		"STUB_STATE="+h.stubDir,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("bash: %v\n%s", err, out)
	}
	return string(out)
}

// privilegedLog returns the recorded privileged calls, in order.
func (h *devDNSHarness) privilegedLog() []string {
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

func (h *devDNSHarness) backupPath(name string) string {
	return filepath.Join(h.stateDir, name)
}

// assertLogged fails unless want appears verbatim in the recorded privileged calls.
func assertLogged(t *testing.T, log []string, want string) {
	t.Helper()
	if slices.Contains(log, want) {
		return
	}
	t.Errorf("privileged call %q was never made; got %v", want, log)
}

// dns_links picks the interfaces that actually carry traffic. Getting this
// wrong points resolved at rolodex on an interface nothing resolves through,
// which looks exactly like the redirect having silently done nothing.
func TestDevDNSLinksParsesDefaultRoutes(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.fixture("ip-4", "default via 192.168.11.1 dev wlan0 proto dhcp metric 600\n")
	h.fixture("ip-6", "default via fe80::1 dev wlan0 proto ra metric 1024\n"+
		"default via fe80::2 dev eth0 proto ra metric 1024\n")

	got := strings.Fields(h.run("dns_links"))

	// wlan0 holds both the v4 and v6 default route and must appear once: the
	// caller loops over this list issuing resolvectl calls per link.
	want := []string{"eth0", "wlan0"}
	if len(got) != len(want) {
		t.Fatalf("dns_links = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dns_links = %v, want %v", got, want)
			break
		}
	}
}

// A link with no default route is not consulted by resolved, so redirecting it
// is wasted work -- and an empty list must not be read as "all links".
func TestDevDNSLinksIsEmptyWithNoDefaultRoute(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)

	if got := strings.TrimSpace(h.run("dns_links")); got != "" {
		t.Errorf("dns_links = %q with no default route, want empty", got)
	}
}

// link_setting strips resolvectl's "Link 2 (eth0): " prefix. Leaving it on
// would write the prefix back into the backup file, and the restore would then
// hand resolvectl a nonsense server list.
func TestDevDNSLinkSettingStripsResolvectlPrefix(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.fixture("resolvectl-dns-eth0", "Link 2 (eth0): 192.168.11.1 1.1.1.1\n")

	if got := strings.TrimSpace(h.run("link_setting dns eth0")); got != "192.168.11.1 1.1.1.1" {
		t.Errorf("link_setting = %q, want %q", got, "192.168.11.1 1.1.1.1")
	}
}

// The redirect must record what it found BEFORE changing it, then point the
// link at rolodex with a `~.` routing domain. Without `~.` resolved is free to
// prefer another link's servers and a .home query leaves for the LAN resolver,
// which is an NXDOMAIN -- the bug that made dev DNS look haunted.
func TestDevDNSRedirectRecordsBackupAndPointsLinksAtRolodex(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.resolvedRunning()
	h.fixture("ip-4", "default via 192.168.11.1 dev wlan0 proto dhcp metric 600\n")
	h.fixture("resolvectl-dns-wlan0", "Link 3 (wlan0): 192.168.11.1\n")
	h.fixture("resolvectl-domain-wlan0", "Link 3 (wlan0): lan\n")

	h.run("redirect_resolved_dns")

	got, err := os.ReadFile(h.backupPath("resolved-dns.bak"))
	if err != nil {
		t.Fatalf("read resolved-dns.bak: %v", err)
	}
	if want := "wlan0|192.168.11.1|lan\n"; string(got) != want {
		t.Errorf("backup = %q, want %q -- the restore reads this verbatim", got, want)
	}

	log := h.privilegedLog()
	assertLogged(t, log, "resolvectl dns wlan0 127.0.0.2")
	assertLogged(t, log, "resolvectl domain wlan0 ~.")
	assertLogged(t, log, "resolvectl flush-caches")
}

// Not every box runs resolved. There the redirect is resolv.conf's job alone,
// and writing a backup file would strand a restore that has nothing to undo.
func TestDevDNSRedirectSkippedWhenResolvedNotRunning(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.fixture("ip-4", "default via 192.168.11.1 dev wlan0 proto dhcp metric 600\n")

	h.run("redirect_resolved_dns")

	if _, err := os.Stat(h.backupPath("resolved-dns.bak")); !os.IsNotExist(err) {
		t.Errorf("wrote a resolved backup on a box with no resolved (err=%v)", err)
	}
	if log := h.privilegedLog(); len(log) != 0 {
		t.Errorf("issued privileged calls with no resolved running: %v", log)
	}
}

// The restore puts back exactly what was recorded. `resolvectl revert` is NOT
// the restore for a link that had settings: revert drops what NetworkManager
// pushed at connection time and NM does not re-push until the connection is
// reactivated, leaving the link with no DNS at all.
func TestDevDNSRestoreReinstatesRecordedServers(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.resolvedRunning()
	if err := os.WriteFile(h.backupPath("resolved-dns.bak"),
		[]byte("wlan0|192.168.11.1 1.1.1.1|lan\n"), 0o600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}

	h.run("restore_resolved_dns")

	log := h.privilegedLog()
	assertLogged(t, log, "resolvectl dns wlan0 192.168.11.1 1.1.1.1")
	assertLogged(t, log, "resolvectl domain wlan0 lan")
	for _, l := range log {
		if strings.Contains(l, "revert") {
			t.Errorf("reverted a link that had recorded settings (%q): "+
				"revert leaves the link with no DNS until the connection is reactivated", l)
		}
	}

	// Consumed, so a second restore is a no-op rather than a replay.
	if _, err := os.Stat(h.backupPath("resolved-dns.bak")); !os.IsNotExist(err) {
		t.Errorf("backup survived the restore (err=%v); a replay would re-apply stale servers", err)
	}
}

// Revert IS right for a link that genuinely had nothing set -- there is no
// prior value to write back, and leaving rolodex configured would outlive dev.
func TestDevDNSRestoreRevertsLinksThatHadNothing(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.resolvedRunning()
	if err := os.WriteFile(h.backupPath("resolved-dns.bak"), []byte("eth0||\n"), 0o600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}

	h.run("restore_resolved_dns")

	assertLogged(t, h.privilegedLog(), "resolvectl revert eth0")
}

// restore_host_dns is wired into an EXIT trap, three signal traps, `dev-stop`,
// and `dev-restore-dns`, so it runs repeatedly and often with nothing to undo.
// It has to stay silent and harmless there.
func TestDevDNSRestoreIsSafeWithNothingToRestore(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	h.resolvedRunning()

	h.run("restore_host_dns; restore_host_dns")

	if log := h.privilegedLog(); len(log) != 0 {
		t.Errorf("touched host DNS with no backup present: %v", log)
	}
}

// resolv.conf is usually a symlink to resolved's generated stub file. The
// redirect drops the symlink rather than writing through it, so the restore has
// to recreate the link -- writing a plain file there would leave the host
// pointed at a dead rolodex long after dev exited.
func TestDevDNSRestoreRecreatesResolvConfSymlink(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	if err := os.WriteFile(h.backupPath("resolv.conf.bak"),
		[]byte("__SYMLINK__\n/run/systemd/resolve/stub-resolv.conf\n"), 0o600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}

	h.run("restore_resolv_conf")

	log := h.privilegedLog()
	assertLogged(t, log, "rm -f /etc/resolv.conf")
	assertLogged(t, log, "ln -s /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf")
}

// A box whose resolv.conf was a real file gets the file copied back verbatim.
func TestDevDNSRestoreCopiesBackAPlainResolvConf(t *testing.T) {
	t.Parallel()
	h := newDevDNSHarness(t)
	backup := h.backupPath("resolv.conf.bak")
	if err := os.WriteFile(backup, []byte("nameserver 192.168.11.1\n"), 0o600); err != nil {
		t.Fatalf("seed backup: %v", err)
	}

	h.run("restore_resolv_conf")

	assertLogged(t, h.privilegedLog(), "cp "+backup+" /etc/resolv.conf")
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("backup survived the restore (err=%v)", err)
	}
}
