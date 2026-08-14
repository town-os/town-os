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

// TestBothEntryPointsIgnoreMtime is what is left of the mtime guard: nothing.
//
// WriteConfig used to refuse to overwrite a rolodex.yml newer than the
// systemcontroller binary, on the theory that an operator had hand-edited it —
// and the file written at the previous boot ALWAYS satisfies that condition, so
// the guard froze the file permanently instead. RewriteConfig existed only to
// route around it. Both names now reconcile, and a settings change reaching
// rolodex must not depend on which one the caller reached for.
func TestBothEntryPointsIgnoreMtime(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		write func(*Manager) (bool, error)
	}{
		{name: "WriteConfig", write: (*Manager).WriteConfig},
		{name: "RewriteConfig", write: (*Manager).RewriteConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			m := NewManager(Config{DataDir: dir})

			if _, err := m.RewriteConfig(); err != nil {
				t.Fatalf("seed RewriteConfig: %v", err)
			}

			// Far newer than any binary — what the old guard keyed on, and the
			// state every boot leaves behind.
			path := filepath.Join(dir, "rolodex.yml")
			future := time.Now().Add(24 * time.Hour)
			if err := os.Chtimes(path, future, future); err != nil {
				t.Fatalf("Chtimes: %v", err)
			}

			m.SetResolutionMode(ResolutionModeForward)

			written, err := tc.write(m)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !written {
				t.Fatalf("%s skipped a config newer than the binary", tc.name)
			}
			assertConfigMode(t, dir, ResolutionModeForward)
		})
	}
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

	data, err := os.ReadFile(filepath.Join(dir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read rolodex.yml: %v", err)
	}
	if !strings.Contains(string(data), "  mode: "+want) {
		t.Fatalf("rolodex.yml does not select mode %q:\n%s", want, data)
	}
}
