// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"os"
	"os/exec"
	"testing"

	"gitea.com/town-os/town-os/src/caddysup"
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
