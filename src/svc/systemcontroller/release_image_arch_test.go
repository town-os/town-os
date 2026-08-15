// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Release images are built to an arch-suffixed staging name (staged_ref in
// make/lib.sh, `<image>:local-<arch>`) and every published tag is made from it
// through tag_from_staged, which verifies the architecture before it tags.
//
// The rule exists because the absence of it shipped. Every release image except
// the systemcontroller was built as the bare `quay.io/town/<name>`, which podman
// stores as `:latest` -- ONE slot per image, shared by both architectures -- and
// the push targets ran `podman tag <image> <image>:rc.latest-$ARCH`, which names
// no architecture anywhere and so tags whatever sits in that slot at that
// instant. Build aarch64 and x86_64 in the same checkout and the second build
// overwrites the first's slot, so `rc.latest-x86_64` went out holding arm64
// binaries for ingress, networkcontroller and ui. Those services crash-looped on
// boot with `exec container process: Exec format error`, and nothing failed at
// push time to say so.
//
// build_push_targets_test.go covers WHICH images each target ships; this file
// covers which BYTES those tags name. Both are static-plus-stub by necessity:
// the failure is invisible at the moment it is made, and only a box that pulls
// the tag ever sees it.
//
// These tests are host-side because the thing under test is a host-side make
// script -- the integration binary runs INSIDE the test container, where the
// repo is not mounted and make/lib.sh does not exist (the same reasoning as
// bun_cache_script_test.go and dev_restore_dns_test.go). The shell functions are
// exercised against a stub podman on PATH with SUDO cleared, so nothing here
// reaches a registry, the image store, or root.

// stagedImageVars is every image a release build produces. It is
// releaseImageVars (build_push_targets_test.go) plus proton, which is gated
// behind PROTON_ENABLED for pushing but whose build arm exists unconditionally
// and must stage like the rest.
var stagedImageVars = append(slices.Clone(releaseImageVars), "RELEASE_PROTON_IMAGE")

// TestReleaseBuildsTagTheStagedRef asserts each release build writes the
// arch-suffixed staging name rather than the bare image name. A bare
// `-t "${RELEASE_X_IMAGE}"` is the shared `:latest` slot both architectures
// write, and it is what made an arm64 image reachable under an x86_64 tag.
func TestReleaseBuildsTagTheStagedRef(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)
	for _, v := range stagedImageVars {
		want := `-t "$(staged_ref "${` + v + `}")"`
		if !strings.Contains(script, want) {
			t.Errorf("%s: no release build tags %s through staged_ref (expected %s); a build "+
				"naming the image without its architecture writes the :latest slot the other "+
				"architecture also writes", buildScript, v, want)
		}
	}
}

// bareTagRe matches a podman tag or build that names a release image with no
// architecture -- `"${RELEASE_UI_IMAGE}"` on its own, as opposed to
// `"${RELEASE_UI_IMAGE}:rc.latest-${ARCH}"` (a destination, which is fine) or
// the staged_ref form.
var bareTagRe = regexp.MustCompile(`(podman tag|-t) "\$\{RELEASE_[A-Z_]*IMAGE\}"`)

// TestPushTargetsNeverTagFromABareReleaseName is the regression guard proper.
// The bug was not a typo in one target: it was that tagging from an
// arch-agnostic name was expressible at all, was written in six places, and read
// perfectly fine in review.
func TestPushTargetsNeverTagFromABareReleaseName(t *testing.T) {
	t.Parallel()

	for i, line := range strings.Split(readRepoFile(t, buildScript), "\n") {
		if bareTagRe.MatchString(line) {
			t.Errorf("%s:%d tags from a name with no architecture: %s\nThat name is podman's "+
				":latest slot, shared by every architecture built in this checkout. Use "+
				"tag_from_staged, or staged_ref for a build -t.", buildScript, i+1, strings.TrimSpace(line))
		}
	}
}

// TestTagFromStagedGuardsEveryTag asserts the check and the tag stay a single
// operation. A guard the caller has to remember to call is a guard that gets
// skipped the next time somebody adds an image.
func TestTagFromStagedGuardsEveryTag(t *testing.T) {
	t.Parallel()

	body := shellFuncBody(t, readRepoFile(t, "make/lib.sh"), "tag_from_staged")
	if !strings.Contains(body, "assert_image_arch") {
		t.Error("make/lib.sh: tag_from_staged does not call assert_image_arch; an unchecked " +
			"tag is invisible at push time and surfaces as an unbootable service on a box")
	}
	if !strings.Contains(body, "staged_ref") {
		t.Error("make/lib.sh: tag_from_staged does not tag from staged_ref, so it names no architecture")
	}
}

// podmanStub answers `image inspect --format {{.Architecture}}` with STUB_ARCH
// (unset means the image is not in local storage) and records every tag it is
// asked to make, so a test can assert a refused tag was never made.
const podmanStub = `#!/bin/bash
case "$1 $2" in
  "image inspect")
    if [ -z "${STUB_ARCH:-}" ]; then
      echo "Error: no such image" >&2
      exit 1
    fi
    echo "${STUB_ARCH}"
    ;;
  "tag "*)
    echo "$2 $3" >> "${STUB_RECORD}/tags"
    ;;
esac
`

// runLib sources make/lib.sh with the stub podman on PATH and runs snippet,
// returning its combined output, the stub's record directory, and the error.
func runLib(t *testing.T, snippet string, env ...string) (string, string, error) {
	t.Helper()
	return runLibWithStub(t, podmanStub, snippet, env...)
}

// runLibWithStub is runLib against a caller-supplied podman stub, so a test
// that needs podman to model something other than tagging can say so without
// growing this one stub into a second implementation of podman.
func runLibWithStub(t *testing.T, stub, snippet string, env ...string) (string, string, error) {
	t.Helper()

	stubDir := t.TempDir()
	recordDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "podman"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}

	// The reads here are local files and one stub process, so this bound only
	// exists so a wedged bash cannot hang the whole run.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// SUDO="" both before and after sourcing, for the same reason
	// bun_cache_script_test.go does it: no privileged call is reachable at all,
	// so nothing in this test can touch the machine's own image store.
	cmd := exec.CommandContext(ctx, "bash", "-c", `SUDO=""; . make/lib.sh; SUDO=""; `+snippet)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_RECORD="+recordDir,
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), recordDir, err
}

// stubTags returns the tags the stub was asked to make, or "" when it was never
// asked -- the tag never happened.
func stubTags(t *testing.T, recordDir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(recordDir, "tags"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read stub tag record: %v", err)
	}
	return string(body)
}

// TestStagedRefNamesTheArchitecture asserts the staging name carries the arch
// this invocation builds for. That is the whole mechanism: two builds in one
// checkout write two different names and cannot see each other.
func TestStagedRefNamesTheArchitecture(t *testing.T) {
	t.Parallel()

	for _, arch := range []string{"x86_64", "aarch64"} {
		out, _, err := runLib(t, `staged_ref quay.io/town/town`, "BUILD_ARCH="+arch)
		if err != nil {
			t.Fatalf("staged_ref (BUILD_ARCH=%s): %v\n%s", arch, err, out)
		}
		want := "quay.io/town/town:local-" + arch + "\n"
		if out != want {
			t.Errorf("staged_ref with BUILD_ARCH=%s = %q, want %q; a staging name without the "+
				"architecture is the shared slot again", arch, out, want)
		}
	}
}

// TestTagFromStagedTagsTheStagedRefOnAnArchMatch is the working path: the
// staging image holds the architecture being built, so the published tag is made
// from it and from nothing else.
func TestTagFromStagedTagsTheStagedRefOnAnArchMatch(t *testing.T) {
	t.Parallel()

	out, rec, err := runLib(t, `tag_from_staged quay.io/town/ui rc.latest-x86_64`,
		"BUILD_ARCH=x86_64", "STUB_ARCH=amd64")
	if err != nil {
		t.Fatalf("tag_from_staged on a matching arch failed: %v\n%s", err, out)
	}
	want := "quay.io/town/ui:local-x86_64 quay.io/town/ui:rc.latest-x86_64\n"
	if got := stubTags(t, rec); got != want {
		t.Errorf("tagged %q, want %q", got, want)
	}
}

// TestTagFromStagedRefusesAnArchMismatch plants the exact wreckage that shipped
// -- an arm64 image sitting where an x86_64 build expects its own -- and asserts
// the push fails here rather than on every box that pulls the tag.
func TestTagFromStagedRefusesAnArchMismatch(t *testing.T) {
	t.Parallel()

	out, rec, err := runLib(t, `tag_from_staged quay.io/town/ingress rc.latest-x86_64`,
		"BUILD_ARCH=x86_64", "STUB_ARCH=arm64")
	if err == nil {
		t.Fatalf("tag_from_staged tagged an arm64 image as x86_64:\n%s", out)
	}
	if tags := stubTags(t, rec); tags != "" {
		t.Errorf("the tag was made anyway: %q", tags)
	}
	// The refusal has to name both architectures. A push that fails without
	// saying what it found is a push somebody retries.
	if !strings.Contains(out, "arm64") || !strings.Contains(out, "amd64") {
		t.Errorf("the refusal names neither the arch found nor the arch wanted:\n%s", out)
	}
}

// TestAssertImageArchNamesAMissingImage covers the other failure: nothing built
// the staging image, so there is nothing to inspect. "not in local storage" is a
// different repair from a mismatch and has to read differently.
func TestAssertImageArchNamesAMissingImage(t *testing.T) {
	t.Parallel()

	// No STUB_ARCH, so the inspect fails the way podman fails on an absent image.
	out, rec, err := runLib(t, `assert_image_arch quay.io/town/gfeh:local-x86_64`, "BUILD_ARCH=x86_64")
	if err == nil {
		t.Fatalf("assert_image_arch passed an image that is not in local storage:\n%s", out)
	}
	if !strings.Contains(out, "not in local storage") {
		t.Errorf("the failure does not say the image was never built:\n%s", out)
	}
	if tags := stubTags(t, rec); tags != "" {
		t.Errorf("a tag was attempted on a missing image: %q", tags)
	}
}

// shellFuncBody returns a shell function's body, from its opening line to the
// first line that is a bare closing brace.
func shellFuncBody(t *testing.T, script, name string) string {
	t.Helper()
	start := strings.Index(script, name+"() {")
	if start < 0 {
		t.Fatalf("shell function %s not found", name)
	}
	body := script[start:]
	if idx := strings.Index(body, "\n}"); idx >= 0 {
		body = body[:idx]
	}
	return body
}
