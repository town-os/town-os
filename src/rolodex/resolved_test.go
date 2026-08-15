package rolodex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ResolvedDropInPath is a fixed package variable and the integration harness
// asserts on the file's absence, so its value is pinned here: a path that moved
// would turn that check into one that can never fail.
func TestResolvedDropInPathIsTheSystemdDropIn(t *testing.T) {
	t.Parallel()

	const want = "/etc/systemd/resolved.conf.d/town-os.conf"
	if ResolvedDropInPath != want {
		t.Errorf("ResolvedDropInPath = %q, want %q", ResolvedDropInPath, want)
	}
}

// Every test below goes through writeResolvedDropIn with a temp directory
// rather than through ConfigureResolvedRouting. That is not a style choice:
// ConfigureResolvedRouting writes the real /etc/systemd/resolved.conf.d and
// SIGHUPs the real systemd-resolved, and these tests run on the developer's own
// machine, where that is the resolver the machine is using. A test that calls it
// passes only by accident of not running as root.
func TestWriteResolvedDropInWritesTheRoutingStanza(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	changed, err := writeResolvedDropIn(dir, "home", DNSLoopback)
	if err != nil {
		t.Fatalf("writeResolvedDropIn: %v", err)
	}
	if !changed {
		t.Error("changed = false writing a drop-in that did not exist")
	}

	got, err := os.ReadFile(filepath.Join(dir, "town-os.conf"))
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	want := "[Resolve]\nDNS=" + DNSLoopback + "\nDomains=~home\n"
	if string(got) != want {
		t.Errorf("drop-in content:\ngot:  %q\nwant: %q", got, want)
	}
}

// The changed report is what decides whether resolved gets SIGHUPed, so a
// rewrite of identical content has to come back false: a boot that reloaded the
// machine's resolver every time would drop in-flight queries for no reason.
func TestWriteResolvedDropInReportsUnchangedContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, err := writeResolvedDropIn(dir, "home", DNSLoopback); err != nil {
		t.Fatalf("first write: %v", err)
	}

	changed, err := writeResolvedDropIn(dir, "home", DNSLoopback)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if changed {
		t.Error("changed = true rewriting identical content; resolved would be signalled on every boot")
	}
}

// A TLD change has to be reported as changed, or resolved goes on routing the
// old domain to rolodex and the new one nowhere.
func TestWriteResolvedDropInReportsANewTLD(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, err := writeResolvedDropIn(dir, "home", DNSLoopback); err != nil {
		t.Fatalf("first write: %v", err)
	}

	changed, err := writeResolvedDropIn(dir, "local", DNSLoopback)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if !changed {
		t.Fatal("changed = false after the TLD moved; resolved would keep routing the old one")
	}

	got, err := os.ReadFile(filepath.Join(dir, "town-os.conf"))
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	if want := "[Resolve]\nDNS=" + DNSLoopback + "\nDomains=~local\n"; string(got) != want {
		t.Errorf("drop-in content:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestWriteResolvedDropInSkipsEmptyTLD(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	changed, err := writeResolvedDropIn(dir, "", DNSLoopback)
	if err != nil {
		t.Fatalf("writeResolvedDropIn: %v", err)
	}
	if changed {
		t.Error("changed = true with no TLD")
	}
	assertNoDropIn(t, dir)
}

func TestWriteResolvedDropInSkipsEmptyAddr(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	changed, err := writeResolvedDropIn(dir, "home", "")
	if err != nil {
		t.Fatalf("writeResolvedDropIn: %v", err)
	}
	if changed {
		t.Error("changed = true with no loopback address")
	}
	assertNoDropIn(t, dir)
}

// The whole point of returning an error rather than logging inside the writer is
// that the caller can stay non-fatal while the test can still see the failure.
func TestWriteResolvedDropInReportsAnUnwritableDir(t *testing.T) {
	t.Parallel()

	// A regular file where the directory should be: MkdirAll cannot turn it
	// into one, and this needs no privilege to arrange.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	changed, err := writeResolvedDropIn(blocked, "home", DNSLoopback)
	if err == nil {
		t.Fatal("writeResolvedDropIn = nil error with a file in place of the drop-in dir")
	}
	if changed {
		t.Error("changed = true on a failed write")
	}
}

// ConfigureResolvedRouting itself is only ever exercised on the arguments that
// make it return before it touches the filesystem — see the note above.
func TestConfigureResolvedRoutingSkipsEmptyTLD(t *testing.T) {
	t.Parallel()
	// Should not panic or do anything when TLD is empty.
	ConfigureResolvedRouting(context.Background(), "", DNSLoopback)
}

func TestConfigureResolvedRoutingSkipsEmptyAddr(t *testing.T) {
	t.Parallel()
	// Should not panic or do anything when loopback addr is empty.
	ConfigureResolvedRouting(context.Background(), "home", "")
}

func TestConfigureResolvedRoutingRespectsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	// Must not panic with a cancelled context. Empty TLD keeps this off the
	// real /etc while still running the function.
	ConfigureResolvedRouting(ctx, "", DNSLoopback)
}

func assertNoDropIn(t *testing.T, dir string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(dir, "town-os.conf")); err == nil {
		t.Error("a drop-in was written for an incomplete configuration")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat drop-in: %v", err)
	}
}
