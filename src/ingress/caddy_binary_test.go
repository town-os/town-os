// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/caddysup"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// findCaddy locates a caddy binary for the two tests in this package that ask
// the real thing whether a rendered Caddyfile is loadable.
//
// Those two stayed here when the tests that drive a serving caddy moved to
// integration/ (see integration/ingress_caddy_test.go for why they moved):
// these assert on the output of renderCaddyfile, which is unexported, so they
// can only run from inside the package. What that costs is real and worth
// naming — ./src/... runs on the HOST, so on a box without caddy these two
// skip, and only the integration suite proves caddy accepts what this renderer
// emits. They are worth keeping anyway: `caddy validate` is the same parse
// `caddy reload` performs, and they cover a case the serving tests do not —
// that ONE malformed hostname cannot make caddy reject the whole config and
// leave every other route frozen at its last good state.
func findCaddy(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("caddy"); err == nil {
		return p
	}
	if _, err := os.Stat(caddysup.DefaultCaddyBinary); err == nil {
		return caddysup.DefaultCaddyBinary
	}
	t.Skip("caddy binary not found; skipping the config-validation tests")
	return ""
}

// testLeafDir issues a real local-CA leaf for host and returns the directory
// the renderer will point a `tls` directive at.
//
// Every test that hands a config to caddy needs this, because `caddy validate`
// does not stop at parsing: it provisions the TLS app, which OPENS every
// certificate the config names. A route whose CertDir is a plausible-looking
// path that does not exist fails with `open /c/gitea/cert.pem: no such file or
// directory` — a failure about the fixture, reported as though the config were
// malformed. The tests here could not notice: they skip on every box without a
// caddy binary, which is every developer box, so the fixture was wrong for as
// long as it was never run.
//
// One CA per call is deliberate: these tests validate one config each, and a
// shared CA would be state between them.
func testLeafDir(t *testing.T, host string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "leaf")
	ca, err := townostls.EnsureCA(t.TempDir())
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir leaf dir: %v", err)
	}
	if err := ca.IssueLeaf(dir, []string{host}); err != nil {
		t.Fatalf("IssueLeaf %s: %v", host, err)
	}
	return dir
}
