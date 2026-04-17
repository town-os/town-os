//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProtonContainerfileProvidesCABundle is the proton-build counterpart of
// TestContainerfilesProvideCABundle: when the proton runner is built in,
// Containerfile.proton must also ship a CA bundle, since the runner makes
// outbound HTTPS calls (e.g. Steam compatibility data fetches).
func TestProtonContainerfileProvidesCABundle(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	p := "Containerfile.proton"
	b, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	if !strings.Contains(string(b), "ca-certificates") {
		t.Fatalf("%s must install ca-certificates in its final stage", p)
	}
}
