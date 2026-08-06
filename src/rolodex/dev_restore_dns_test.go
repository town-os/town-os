// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests cover make/dev.sh's host-DNS restore, the shell twin of
// resolved.go: both point the host's resolver at DNSLoopback and both have to
// be able to put it back. They live here rather than in ./integration because
// the integration binary runs INSIDE the test container, where the repo is not
// mounted and make/dev.sh does not exist — a host-side make script can only be
// tested by a host-side test, and `go test ./src/...` is the host-side run.
//
// Nothing here touches the machine's real configuration. dev.sh reads its two
// host paths from TOWN_OS_RESOLV_CONF and TOWN_OS_STATE_GLOB (defaulting to
// /etc/resolv.conf and /tmp/town-os-*), both redirected into t.TempDir(), and
// the harness clears SUDO because every target is a file the test owns. A test
// that could reach the real /etc/resolv.conf is a test that can take the
// machine running it off the network, which is the exact failure this code
// exists to repair.
//
// Only a resolv.conf.bak is ever written. Restoring the systemd-resolved half
// means running `resolvectl dns <link> ...` against the live host, so leaving
// resolved-dns.bak absent makes restore_resolved_dns return on its first line.

// devScriptRoot is the repo root, which make/dev.sh must run from: it sources
// make/lib.sh by relative path. Tests in src/rolodex run two levels down.
func devScriptRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "make", "dev.sh")); err != nil {
		t.Fatalf("stat make/dev.sh: %v", err)
	}
	return root
}

// restoreDNSEnv is one sandboxed invocation of the restore path.
type restoreDNSEnv struct {
	stateDir   string // the invoking checkout's STATE_DIR
	orphanRoot string // holds the town-os-*/ dirs the scan walks
	resolvConf string // stands in for /etc/resolv.conf
}

func newRestoreDNSEnv(t *testing.T) restoreDNSEnv {
	t.Helper()
	base := t.TempDir()
	e := restoreDNSEnv{
		stateDir:   filepath.Join(base, "state"),
		orphanRoot: filepath.Join(base, "orphans"),
		resolvConf: filepath.Join(base, "resolv.conf"),
	}
	for _, dir := range []string{e.stateDir, e.orphanRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}
	// The host as a dead `make dev` left it: pointed at rolodex, nothing else.
	e.writeResolvConf(t, "nameserver "+DNSLoopback+"\n")
	return e
}

func (e restoreDNSEnv) writeResolvConf(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(e.resolvConf, []byte(content), 0o644); err != nil {
		t.Fatalf("write resolv.conf: %v", err)
	}
}

// orphan writes a leftover backup under a fake instance directory, stamped with
// modTime so "newest wins" is deterministic rather than filesystem-dependent.
func (e restoreDNSEnv) orphan(t *testing.T, instance, content string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(e.orphanRoot, "town-os-"+instance)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	path := filepath.Join(dir, "resolv.conf.bak")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write orphan backup: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("Chtimes %s: %v", path, err)
	}
	return path
}

// ownBackup writes a backup belonging to the invoking checkout itself.
func (e restoreDNSEnv) ownBackup(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(e.stateDir, "resolv.conf.bak")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write own backup: %v", err)
	}
	return path
}

// run sources make/dev.sh (which defines the functions and stops, rather than
// dispatching) and drives the same two calls the restore-dns target makes.
// Clearing SUDO keeps the test from shelling out to sudo for a `cp` onto a file
// it already owns, so the suite never prompts for a password.
func (e restoreDNSEnv) run(t *testing.T) string {
	t.Helper()
	const script = `
set -e
. make/dev.sh
SUDO=
adopt_orphan_dns_backup
restore_host_dns
`
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = devScriptRoot(t)
	cmd.Env = append(os.Environ(),
		"STATE_DIR="+e.stateDir,
		"TOWN_OS_RESOLV_CONF="+e.resolvConf,
		"TOWN_OS_STATE_GLOB="+filepath.Join(e.orphanRoot, "town-os-*"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dev.sh restore: %v\n%s", err, out)
	}
	return string(out)
}

func (e restoreDNSEnv) currentResolvConf(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.resolvConf)
	if err != nil {
		t.Fatalf("read resolv.conf: %v", err)
	}
	return string(b)
}

// TestDevRestoreDNSAdoptsOrphanedBackup is the regression test for a box left
// with no working resolver at all.
//
// STATE_DIR is keyed to the md5 of the working directory, so the backup a
// `make dev` took is reachable only from the directory that took it. Delete
// that worktree — the normal end of a worktree's life — and `make
// dev-restore-dns` from every surviving checkout exits 0 having done nothing
// (restore_resolv_conf opens with `[ -f "${DNS_BACKUP}" ] || return 0`), while
// /etc/resolv.conf still names a rolodex that is gone. Every lookup on the
// machine then fails, which surfaces as unrelated-looking `lookup
// registry-1.docker.io: no such host` errors partway through an image pull.
func TestDevRestoreDNSAdoptsOrphanedBackup(t *testing.T) {
	t.Parallel()
	e := newRestoreDNSEnv(t)

	const want = "# Generated by NetworkManager\nsearch example.edu\nnameserver 10.9.8.7\n"
	orphan := e.orphan(t, "f0d3dbfe", want, time.Now().Add(-time.Hour))

	e.run(t)

	if got := e.currentResolvConf(t); got != want {
		t.Fatalf("resolv.conf was not restored from the orphan\n got: %q\nwant: %q", got, want)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("an adopted backup must be consumed, but %s still exists (err=%v)", orphan, err)
	}
}

// TestDevRestoreDNSAdoptsNewestOrphan pins the tie-break. A machine
// accumulates one state dir per checkout that ever ran dev, so more than one
// leftover is the normal case, and restoring the wrong one puts back whatever
// resolver config that checkout last saw.
func TestDevRestoreDNSAdoptsNewestOrphan(t *testing.T) {
	t.Parallel()
	e := newRestoreDNSEnv(t)

	const stale = "nameserver 10.0.0.1\n"
	const fresh = "nameserver 10.0.0.99\n"
	staleBak := e.orphan(t, "aaaaaaaa", stale, time.Now().Add(-72*time.Hour))
	e.orphan(t, "bbbbbbbb", fresh, time.Now().Add(-time.Minute))

	e.run(t)

	if got := e.currentResolvConf(t); got != fresh {
		t.Fatalf("expected the newest orphan to win\n got: %q\nwant: %q", got, fresh)
	}
	if _, err := os.Stat(staleBak); err != nil {
		t.Errorf("the older orphan must be left alone, got err=%v", err)
	}
}

// TestDevRestoreDNSPrefersOwnBackup pins the first guard: a backup belonging to
// this checkout always wins and the scan never runs. Adopting an orphan while a
// real backup exists would restore a different machine state than the one this
// checkout redirected away from.
func TestDevRestoreDNSPrefersOwnBackup(t *testing.T) {
	t.Parallel()
	e := newRestoreDNSEnv(t)

	const mine = "nameserver 192.168.1.1\n"
	e.ownBackup(t, mine)
	orphan := e.orphan(t, "cccccccc", "nameserver 10.0.0.1\n", time.Now())

	out := e.run(t)

	if got := e.currentResolvConf(t); got != mine {
		t.Fatalf("own backup must win\n got: %q\nwant: %q", got, mine)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("the orphan must be untouched when this checkout has its own, got err=%v", err)
	}
	if strings.Contains(out, "adopting the orphan") {
		t.Errorf("the scan must not run when this checkout has its own backup:\n%s", out)
	}
}

// TestDevRestoreDNSNoBackupIsNoOp pins that the escape hatch stays safe on a
// machine that never redirected anything: it must succeed and change nothing,
// since it is what a confused operator reaches for first.
func TestDevRestoreDNSNoBackupIsNoOp(t *testing.T) {
	t.Parallel()
	e := newRestoreDNSEnv(t)

	before := e.currentResolvConf(t)
	e.run(t)

	if got := e.currentResolvConf(t); got != before {
		t.Fatalf("resolv.conf changed with no backup present\n got: %q\nwant: %q", got, before)
	}
}

// TestDevRestoreDNSLeavesStaleOrphanOnHealthyHost pins the second guard: an
// orphan is adopted only while the host is STILL pointed at rolodex. A backup
// left by a run that DID restore is stale, and copying it over a working
// resolv.conf would break the very thing the escape hatch repairs.
//
// host_dns_redirected deliberately consults the live systemd-resolved as well
// as the resolv.conf path — a resolved box can have an innocent resolv.conf and
// its links redirected, the split that makes dev DNS look haunted — and nothing
// stubs resolvectl. So this case is observable only on a host that is not
// itself redirected right now; skip rather than assert something the machine
// decides.
func TestDevRestoreDNSLeavesStaleOrphanOnHealthyHost(t *testing.T) {
	t.Parallel()
	probeCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if hostResolvesThroughRolodex(probeCtx) {
		t.Skip("this host's own resolver points at rolodex; the negative case is unobservable here")
	}

	e := newRestoreDNSEnv(t)
	const healthy = "nameserver 127.0.0.53\n"
	e.writeResolvConf(t, healthy)
	orphan := e.orphan(t, "dddddddd", "nameserver 10.0.0.1\n", time.Now())

	e.run(t)

	if got := e.currentResolvConf(t); got != healthy {
		t.Fatalf("a healthy resolv.conf must not be overwritten from a stale orphan\n got: %q\nwant: %q", got, healthy)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("the stale orphan must be left in place, got err=%v", err)
	}
}

// hostResolvesThroughRolodex reports whether the machine running the tests has
// its own name resolution pointed at DNSLoopback, by either half of the
// redirect — the same question host_dns_redirected asks in shell.
func hostResolvesThroughRolodex(ctx context.Context) bool {
	if b, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "nameserver" && f[1] == DNSLoopback {
				return true
			}
		}
	}
	out, err := exec.CommandContext(ctx, "resolvectl", "status").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Current DNS Server: "+DNSLoopback)
}
