// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestContainerfilesProvideCABundle is a regression test for commit b5c309f.
//
// Background: a pre-b5c309f quay.io/town/town:testing was shipped to the
// running town-os.local box without /etc/ssl/certs/ca-certificates.crt.
// Every outbound HTTPS call inside the systemcontroller container failed
// with "x509: certificate signed by unknown authority". Most of those
// failures were loud (repository git clones warn), but fetchExternalIP
// logged only at slog.Debug — silently dropping every ipinfo.io
// response, leaving the dashboard's external_ip empty for days until an
// operator noticed.
//
// This test asserts that every runtime Containerfile in the repo either
// installs ca-certificates explicitly or declares a final base image
// known to ship one. Static analysis keeps the assertion fast and
// hermetic (no container build, no network) — fine-grained enough to
// catch someone silently dropping `ca-certificates` from an apt/apk
// install line during a routine cleanup.
func TestContainerfilesProvideCABundle(t *testing.T) {
	t.Parallel()

	// Final base images known to ship a CA bundle in their own layers.
	// If one of these is used as the final FROM in a Containerfile, an
	// explicit ca-certificates install is not required.
	//
	// Matching is a plain prefix check against the text after "FROM ",
	// so both tagged ("oven/bun:latest") and untagged forms are covered.
	baseImagesWithCABundle := []string{
		"docker.io/library/caddy",
		"docker.io/library/caddy:",
		"caddy:",
		"caddy ",
		"oven/bun",
	}

	// Every Containerfile whose final stage runs Town OS code that makes
	// outbound HTTPS calls. Adjust if new Containerfiles are added. A
	// Containerfile missing from this list will not be caught here.
	paths := []string{
		"Containerfile",
		"Containerfile.networkcontroller",
		"Containerfile.proton",
		"Containerfile.ui",
		"integration/testdata/Containerfile.dev",
		"integration/testdata/Containerfile.systemd",
		"integration/testdata/Containerfile.ui-integration",
	}

	root := repoRoot(t)
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			b, err := os.ReadFile(filepath.Join(root, p))
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			content := string(b)

			// A matching `ca-certificates` anywhere in any RUN line is
			// sufficient — runtime images that install it in an earlier
			// stage and COPY the bundle over still have it in the final
			// image, and the more common case is just `apt/apk install
			// ca-certificates` in the final stage.
			if strings.Contains(content, "ca-certificates") {
				return
			}

			finalFROM := finalFromLine(content)
			for _, base := range baseImagesWithCABundle {
				if strings.Contains(finalFROM, base) {
					return
				}
			}

			t.Fatalf(
				"%s does not install ca-certificates and its final base (%q) is not in the known-safe list.\n"+
					"Fix: add `ca-certificates` to the final stage's install command (apt on debian, apk on alpine), "+
					"or — if the new base image really does ship one — add its name to baseImagesWithCABundle above.",
				p, strings.TrimSpace(finalFROM),
			)
		})
	}
}

// finalFromLine returns the last "FROM ..." line from the Containerfile's
// source text, without the "FROM " prefix. Multi-stage builds are
// expressed as multiple FROM statements; only the last stage ends up in
// the shipped image.
func finalFromLine(content string) string {
	lines := strings.Split(content, "\n")
	last := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if rest, ok := strings.CutPrefix(line, "FROM "); ok {
			last = rest
		}
	}
	return last
}

// repoRoot walks up from this test file until it finds a directory
// containing go.mod. Used so the test works regardless of the go-test
// working directory and without embedding files into the test binary.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate repo root (no go.mod found within 10 levels)")
	return ""
}
