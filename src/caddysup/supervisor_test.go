// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package caddysup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeFakeCaddy writes an executable stub that stands in for the caddy binary.
// On `run` it blocks (so the supervisor keeps a live child), and on `reload` it
// fails its first failReloads invocations (writing to a per-test counter file)
// and succeeds thereafter — emulating a caddy whose admin API is not yet
// listening right after spawn. failReloads < 0 makes every reload fail.
func writeFakeCaddy(t *testing.T, failReloads int) string {
	t.Helper()
	dir := t.TempDir()
	counter := filepath.Join(dir, "reload-count")
	bin := filepath.Join(dir, "fake-caddy")
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"run) exec sleep 30 ;;\n" +
		"reload)\n" +
		"  n=$(cat '" + counter + "' 2>/dev/null || echo 0)\n" +
		"  n=$((n + 1))\n" +
		"  echo \"$n\" > '" + counter + "'\n" +
		"  if [ " + strconv.Itoa(failReloads) + " -lt 0 ] || [ \"$n\" -le " + strconv.Itoa(failReloads) + " ]; then\n" +
		"    echo 'fake caddy: admin not ready' >&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // G306 -- test stub must be executable
		t.Fatalf("write fake caddy: %v", err)
	}
	return bin
}

// newTestSupervisor builds a production supervisor pointed at a fake caddy, with
// a seeded empty config on disk and short retry timings so tests run fast.
func newTestSupervisor(t *testing.T, failReloads int, deadline, delay time.Duration) *osCaddySupervisor {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(cfg, []byte(":0 {\n}\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	s := &osCaddySupervisor{
		binary:           writeFakeCaddy(t, failReloads),
		configPath:       cfg,
		reloadDeadline:   deadline,
		reloadRetryDelay: delay,
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(); err != nil {
			t.Logf("Shutdown: %v", err)
		}
	})
	return s
}

// A caddy that rejects the first two reloads (admin not up yet) then accepts:
// Reload must retry within the deadline and ultimately succeed.
func TestReloadRetriesTransientFailureThenSucceeds(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor(t, 2, 5*time.Second, 10*time.Millisecond)

	if err := s.Reload([]byte(":80 {\n}\n")); err != nil {
		t.Fatalf("Reload should have succeeded after transient failures, got %v", err)
	}
}

// When every reload fails (a genuine config error, not a transient race), Reload
// must give up once the deadline passes and surface the underlying error rather
// than retry forever.
func TestReloadGivesUpAfterDeadline(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor(t, -1, 150*time.Millisecond, 20*time.Millisecond)

	err := s.Reload([]byte(":80 {\n}\n"))
	if err == nil {
		t.Fatal("Reload should have failed when every attempt fails")
	}
	if !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("expected a give-up error, got %v", err)
	}
}
