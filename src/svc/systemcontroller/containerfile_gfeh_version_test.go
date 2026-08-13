// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"strings"
	"testing"
)

// The gfeh image takes whatever gfehd crates.io holds at build time. There is
// no version knob, and these tests exist to keep one from coming back — because
// the last one did active harm rather than merely being unnecessary.
//
// It was a pair: Containerfile.gfeh declared `ARG GFEH_VERSION=0.1.2` with a
// long comment explaining that anything older cannot run under Town OS, and the
// Makefile declared `GFEH_VERSION ?= 0.1.1` and passed it on every build as a
// --build-arg. A --build-arg wins over an ARG default, so every image built
// through make was 0.1.1 — the version the Containerfile documented as fatal —
// and the documentation saying so was sitting in the file being overridden.
//
// Nothing caught it. cargo installs 0.1.1 happily, the image builds, the push
// succeeds, and both suites stand in a fake gfehd. It surfaces only as object
// storage that never starts on a real box.
//
// Static analysis, like containerfile_bun_cache_test.go: no build, no network,
// no podman. The failure being guarded is silent by construction.

// TestGfehImageHasNoVersionPin asserts neither the Containerfile nor the make
// layer reintroduces a version knob.
//
// A pin here is not simply stale-prone: it is a second place for the answer to
// live, and the two places disagreed for the entire life of the last one.
//
// The declarations are matched, not the names: the comments in those files
// explain what was removed and why, and naming a thing in prose is how that
// explanation works. A test that banned the string would ban its own rationale.
func TestGfehImageHasNoVersionPin(t *testing.T) {
	t.Parallel()

	for _, f := range []struct{ path, decl, what string }{
		{"Containerfile.gfeh", "ARG GFEH_VERSION", "an ARG default"},
		{"Containerfile.gfeh", "ARG GFEH_LATEST", "an ARG default"},
		{"Makefile", "GFEH_VERSION ?=", "a make variable"},
		{"Makefile", "GFEH_LATEST ?=", "a make variable"},
		{"make/build.sh", `--build-arg "GFEH_VERSION`, "a --build-arg"},
		{"make/build.sh", `--build-arg "GFEH_LATEST`, "a --build-arg"},
	} {
		if strings.Contains(readRepoFile(t, f.path), f.decl) {
			t.Errorf("%s reintroduces a gfeh version knob as %s (%q); the image builds the current crates.io release", f.path, f.what, f.decl)
		}
	}

	// The export list is the mechanism that carried the Makefile value into
	// build.sh, so a leftover there is a knob half-removed.
	for line := range strings.SplitSeq(readRepoFile(t, "Makefile"), "\n") {
		if strings.HasPrefix(line, "export ") && strings.Contains(line, "GFEH_VERSION") {
			t.Errorf("Makefile still exports a gfeh version knob: %q", line)
		}
	}
}

// TestGfehInstallTakesTheCurrentRelease asserts the cargo invocation names
// neither a version nor a lockfile.
//
// --version is the pin this removed. --locked is the other half of it: it
// resolves dependencies from the published Cargo.lock rather than current ones,
// so an image built "from the latest release" would still carry the dependency
// set frozen at publish time.
func TestGfehInstallTakesTheCurrentRelease(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, "Containerfile.gfeh")
	if !strings.Contains(body, "cargo install gfehd --root /out") {
		t.Error("Containerfile.gfeh does not install gfehd unversioned into /out")
	}
	for _, flag := range []string{"--version", "--locked"} {
		if strings.Contains(body, "cargo install gfehd "+flag) || strings.Contains(body, flag+" \"$GFEH") {
			t.Errorf("Containerfile.gfeh passes %s to cargo install; the build must take the current release", flag)
		}
	}
}

// TestGfehReleaseBuildBypassesTheLayerCache asserts the release build passes
// --no-cache, and that the local fixture build does not.
//
// This is the invariant that makes "current release" true rather than
// aspirational. `cargo install gfehd` is a byte-identical RUN line on every
// build, so its layer is a permanent cache hit: without --no-cache the release
// image would ship whatever crate was current the first time anyone built it,
// and would keep doing so indefinitely, silently, with the build logs showing a
// clean successful build every time.
//
// The local fixture is asserted to stay cached on purpose. It is a prerequisite
// of every test-integration and dev run, it only needs a real gfehd rather than
// today's, and rebuilding the Rust dependency tree per run would cost minutes
// per invocation.
func TestGfehReleaseBuildBypassesTheLayerCache(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)

	release := caseArm(t, script, "release-gfeh")
	if !strings.Contains(release, "--no-cache") {
		t.Error("release-gfeh does not pass --no-cache; podman would serve the first build's crate forever and a release would ship a stale gfehd")
	}

	local := caseArm(t, script, "gfeh-local")
	if strings.Contains(local, "--no-cache") {
		t.Error("gfeh-local passes --no-cache; it is a prerequisite of every test-integration and dev run and would rebuild the Rust dependency tree each time")
	}
}
