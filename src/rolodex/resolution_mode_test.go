// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package rolodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidResolutionMode(t *testing.T) {
	t.Parallel()

	for mode, want := range map[string]bool{
		ResolutionModeAuto:      true,
		ResolutionModeRecursive: true,
		ResolutionModeForward:   true,
		"":                      false,
		"Recursive":             false,
		"iterative":             false,
		"forwarding":            false,
	} {
		if got := ValidResolutionMode(mode); got != want {
			t.Errorf("ValidResolutionMode(%q) = %v, want %v", mode, got, want)
		}
	}
}

// TestResolutionModeDefaultsToAuto pins the fallback chain as the default.
// Bare "recursive" has no fallback: on a network that filters or hijacks
// outbound :53 every external name SERVFAILs, and since the resolver sends one
// un-retransmitted datagram per server, even ordinary packet loss surfaces as
// SERVFAIL. Auto keeps recursion's privacy where the network allows it.
func TestResolutionModeDefaultsToAuto(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{DataDir: t.TempDir()})
	if got := m.ResolutionMode(); got != ResolutionModeAuto {
		t.Fatalf("ResolutionMode() = %q, want %q", got, ResolutionModeAuto)
	}
}

func TestSetResolutionModeChangesRenderedConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(Config{DataDir: dir})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	assertConfigMode(t, dir, ResolutionModeAuto)

	m.SetResolutionMode(ResolutionModeForward)
	if got := m.ResolutionMode(); got != ResolutionModeForward {
		t.Fatalf("ResolutionMode() = %q, want %q", got, ResolutionModeForward)
	}

	written, err := m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	if !written {
		t.Fatal("RewriteConfig must report a write when the mode changed")
	}
	assertConfigMode(t, dir, ResolutionModeForward)
}

// TestRewriteConfigIgnoresMtimeGuard is the whole reason RewriteConfig exists.
// WriteConfig refuses to overwrite a rolodex.yml that is newer than the
// systemcontroller binary (it assumes an operator hand-edited it) — and the file
// written at the previous boot ALWAYS satisfies that condition. If the settings
// handler used WriteConfig, changing dns_resolution_mode would silently no-op.
func TestRewriteConfigIgnoresMtimeGuard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(Config{DataDir: dir})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}

	// Make the config far newer than the binary, which is what trips
	// WriteConfig's guard.
	path := filepath.Join(dir, "rolodex.yml")
	future := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	m.SetResolutionMode(ResolutionModeForward)

	if written, err := m.WriteConfig(); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	} else if written {
		t.Fatal("WriteConfig should have skipped a config newer than the binary")
	}
	assertConfigMode(t, dir, ResolutionModeAuto) // untouched, as expected

	written, err := m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	if !written {
		t.Fatal("RewriteConfig must overwrite regardless of mtime")
	}
	assertConfigMode(t, dir, ResolutionModeForward)
}

func TestRewriteConfigReportsNoWriteWhenUnchanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(Config{DataDir: dir})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	written, err := m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	if written {
		t.Fatal("RewriteConfig must not report a write when the bytes are identical")
	}
}

func TestUnitNameIsTheRolodexSystemService(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{DataDir: t.TempDir()})
	if got, want := m.UnitName(), "town-os-system--rolodex.service"; got != want {
		t.Fatalf("UnitName() = %q, want %q", got, want)
	}
}

func assertConfigMode(t *testing.T, dir, want string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, "rolodex.yml")) //nolint:gosec // test-controlled temp dir
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	if !strings.Contains(string(data), "  mode: "+want) {
		t.Fatalf("rolodex.yml does not select mode %q:\n%s", want, data)
	}
}
