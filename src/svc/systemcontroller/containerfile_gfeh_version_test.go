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
// Nothing caught it. cargo installs 0.1.1 happily, the image builds, and the
// push succeeds. The unit suite stands in a fake gfehd and never runs the
// daemon; the integration and UI suites do run it, but an old daemon still
// starts. It surfaces only as object storage that never starts on a real box.
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

// withoutComments strips `#` comment lines from a build.sh case arm, so a flag
// named in prose is not mistaken for one on a command line. Both gfeh arms
// discuss --no-cache at length in their comments, which is exactly the trap.
func withoutComments(arm string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(arm, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestGfehBuildsDefeatTheLayerCache asserts both gfeh builds break the cache,
// each the way it should.
//
// This is the invariant that makes "current release" true rather than
// aspirational. `cargo install gfehd` is a byte-identical RUN line on every
// build, so its layer is a permanent cache hit. Left alone it would serve
// whatever crate was current the first time anyone built it, indefinitely,
// silently, with the build logs showing a clean successful build every time.
//
// The two builds want different things, so they break it differently:
//
//   - release-gfeh passes --no-cache. Nothing weaker is acceptable in something
//     that ships.
//   - gfeh-local passes a day-granularity GFEH_CACHE_DATE build-arg. It is a
//     prerequisite of every test-integration and dev run, so --no-cache there
//     would recompile the Rust dependency tree on each one — but a pure cache
//     hit would freeze it on whatever gfehd was current the first time it was
//     built on that machine, and the integration and UI suites start real
//     partitions against it.
func TestGfehBuildsDefeatTheLayerCache(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)

	release := withoutComments(caseArm(t, script, "release-gfeh"))
	if !strings.Contains(release, "--no-cache") {
		t.Error("release-gfeh does not pass --no-cache; podman would serve the first build's crate forever and a release would ship a stale gfehd")
	}

	local := withoutComments(caseArm(t, script, "gfeh-local"))
	if strings.Contains(local, "--no-cache") {
		t.Error("gfeh-local passes --no-cache; it is a prerequisite of every test-integration and dev run and would rebuild the Rust dependency tree each time")
	}
	if !strings.Contains(local, `--build-arg "GFEH_CACHE_DATE=`) {
		t.Error("gfeh-local passes no GFEH_CACHE_DATE build-arg; its cargo layer is a permanent cache hit and the fixture would freeze on an old gfehd")
	}
}

// TestGfehCacheDateActuallyBustsTheLayer asserts Containerfile.gfeh both
// declares GFEH_CACHE_DATE and references it inside the cargo RUN, and that the
// reference tolerates the arg being unset.
//
// Two separate traps, and this build hit both in one commit.
//
// The reference is the mechanism: an ARG that is declared but never used does
// not invalidate the layer it precedes, so a bare declaration would look exactly
// like a working cache-bust, pass a build, and do nothing.
//
// The :- is what makes the reference safe. That RUN sets -u, and release-gfeh
// deliberately does not pass this arg — it passes --no-cache, which supersedes
// it. A bare ${GFEH_CACHE_DATE} therefore aborts the RELEASE build with
// "parameter not set" before cargo runs, which is how this shipped broken: the
// local fixture passes the arg and builds fine, so every test and dev run is
// green while push-rc dies.
func TestGfehCacheDateActuallyBustsTheLayer(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, "Containerfile.gfeh")
	if !strings.Contains(body, "ARG GFEH_CACHE_DATE") {
		t.Fatal("Containerfile.gfeh does not declare ARG GFEH_CACHE_DATE")
	}

	_, afterARG, _ := strings.Cut(body, "ARG GFEH_CACHE_DATE")
	runBody, _, ok := strings.Cut(afterARG, "cargo install gfehd")
	if !ok {
		t.Fatal("Containerfile.gfeh has no cargo install after ARG GFEH_CACHE_DATE")
	}
	if !strings.Contains(runBody, "${GFEH_CACHE_DATE") {
		t.Error("Containerfile.gfeh declares GFEH_CACHE_DATE but never references it before the cargo install; an unused ARG does not invalidate the layer, so the daily bust would silently do nothing")
	}
	if strings.Contains(runBody, "${GFEH_CACHE_DATE}") {
		t.Error("Containerfile.gfeh references ${GFEH_CACHE_DATE} without a :- default; the RUN sets -u and release-gfeh does not pass the arg, so the release build aborts with \"parameter not set\"")
	}
	if !strings.Contains(runBody, "${GFEH_CACHE_DATE:-}") {
		t.Error("Containerfile.gfeh does not reference ${GFEH_CACHE_DATE:-}; the reference must survive the arg being unset on the release build")
	}
}
