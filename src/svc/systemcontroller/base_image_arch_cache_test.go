// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Podman's storage holds ONE image per name:tag, with no room for two
// architectures at once. So `FROM docker.io/library/debian:bookworm-slim` in a
// cross build repoints that name at the target arch and the host-arch image
// becomes dangling — unavoidable, and the reason image_cache_tar keys its tars
// by architecture: the copy that was displaced survives as a tar and comes back
// through a local `podman load` instead of a network pull.
//
// That worked in one direction only. The cross build never went through
// ensure_image at all: PULL_ARGS dropped --pull=never and podman fetched the
// target-arch base implicitly, which touches neither the cache it should have
// read nor the cache it should have written. So every alternation between
// architectures re-pulled every runtime base over the network, forever — a
// checkout that had cross-built repeatedly held debian-bookworm-slim-amd64.tar
// and no arm64 tar at all.
//
// stage_runtime_bases (make/build.sh) is the fix: the target-arch base is put
// into storage THROUGH ensure_image before the build asks for it, so it is
// loaded from the cache when one exists and saved to the cache when it does
// not. These tests hold both halves — that the staging happens for every base a
// Containerfile resolves at the target platform, and that ensure_image really
// does prefer the tar and really does write one.
//
// Host-side, like every other test over these scripts: the integration binary
// runs inside the test container, where the repo is not mounted and
// make/lib.sh does not exist. The shell runs against a stub podman with SUDO
// cleared, so nothing here reaches a registry, the image store, or root.

// releaseArmContainerfiles maps each release build arm to the Containerfile it
// builds. Every one of them has toolchain stages pinned to $BUILDPLATFORM and
// runtime stages that are not, which is the distinction the whole mechanism
// turns on.
var releaseArmContainerfiles = map[string]string{
	"release":          "Containerfile",
	"release-ui":       "Containerfile.ui",
	"release-nc":       "Containerfile.networkcontroller",
	"release-ingress":  "Containerfile.ingress",
	"release-gfeh":     "Containerfile.gfeh",
	"release-proton":   "Containerfile.proton",
}

// fromLine captures a FROM's optional --platform and its image reference.
var fromLine = regexp.MustCompile(`(?m)^FROM\s+(?:--platform=(\S+)\s+)?(\S+)`)

// runtimeBases returns the external images a Containerfile resolves at the
// TARGET platform: every bare FROM, minus the ones naming an earlier stage.
//
// A stage carrying `--platform=$BUILDPLATFORM` is excluded by definition — that
// is the toolchain, it runs on this host, it cross-compiles, and it is wanted at
// the host arch no matter what is being built. Staging one of those at the
// target arch would be the same bug pointed the other way.
func runtimeBases(t *testing.T, containerfile string) []string {
	t.Helper()

	body := readRepoFile(t, containerfile)
	stages := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?mi)^FROM\s+.*\sAS\s+(\S+)`).FindAllStringSubmatch(body, -1) {
		stages[m[1]] = true
	}

	var out []string
	seen := map[string]bool{}
	for _, m := range fromLine.FindAllStringSubmatch(body, -1) {
		platform, ref := m[1], m[2]
		if platform != "" || stages[ref] || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no bare-FROM runtime base found; the parse is wrong, not the file", containerfile)
	}
	return out
}

// TestReleaseArmsStageEveryRuntimeBase is the guard that survives a new
// Containerfile stage. The expected set is derived FROM THE CONTAINERFILE, so
// adding a runtime base without staging it fails here rather than on the next
// person's bandwidth.
func TestReleaseArmsStageEveryRuntimeBase(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)
	for arm, containerfile := range releaseArmContainerfiles {
		t.Run(arm, func(t *testing.T) {
			t.Parallel()

			body := caseArm(t, script, arm)
			for _, base := range runtimeBases(t, containerfile) {
				if !strings.Contains(body, "stage_runtime_bases") {
					t.Fatalf("%s: the %q arm never calls stage_runtime_bases, so every switch of TARGET "+
						"re-pulls %s over the network", buildScript, arm, base)
				}
				if !strings.Contains(body, base) {
					t.Errorf("%s: the %q arm does not stage %s, which %s resolves at the target platform",
						buildScript, arm, base, containerfile)
				}
			}
		})
	}
}

// TestStageRuntimeBasesUsesTheBuildArch pins the two things that make the
// staging worth anything: it goes through ensure_image (the only path that
// reads and writes the arch-keyed cache), and it asks for the BUILD arch rather
// than the host's. Staging at the host arch on a cross build would put the
// wrong image in storage under a name claiming otherwise.
func TestStageRuntimeBasesUsesTheBuildArch(t *testing.T) {
	t.Parallel()

	body := shellFuncBody(t, readRepoFile(t, buildScript), "stage_runtime_bases")
	if !strings.Contains(body, "ensure_image") {
		t.Error("make/build.sh: stage_runtime_bases does not call ensure_image, so it neither " +
			"reads nor writes the per-arch cache and the staging is just a pull")
	}
	if !strings.Contains(body, "build_oci_arch") {
		t.Error("make/build.sh: stage_runtime_bases does not use build_oci_arch; staging at the " +
			"host arch on a cross build stores the wrong image under the base's name")
	}
}

// TestEnsureImageLoadsTheCrossArchTarInsteadOfPulling is the behaviour itself,
// against a stub podman: storage holds the host arch, the cache holds a tar for
// the other one, and the request is for the other one. That has to be a load.
func TestEnsureImageLoadsTheCrossArchTarInsteadOfPulling(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	const img = "docker.io/library/debian:bookworm-slim"
	// The name image_cache_tar composes: <safe name>-<arch>.tar.
	writeFakeTar(t, filepath.Join(cache, "debian-bookworm-slim-arm64.tar"))

	out, rec, err := runLibPodman(t, `ensure_image `+img+` arm64`,
		"IMAGE_CACHE="+cache, "STUB_ARCH=amd64", "STUB_LOAD_ARCH=arm64")
	if err != nil {
		t.Fatalf("ensure_image: %v\n%s", err, out)
	}

	if pulls := stubRecord(t, rec, "pulls"); pulls != "" {
		t.Errorf("ensure_image pulled over the network with a cached arm64 tar present: %q\n%s", pulls, out)
	}
	if loads := stubRecord(t, rec, "loads"); !strings.Contains(loads, "debian-bookworm-slim-arm64.tar") {
		t.Errorf("ensure_image did not load the arm64 tar; loads = %q\n%s", loads, out)
	}
}

// TestEnsureImageCachesWhatItPulls is the half whose absence caused this. The
// first cross build has no tar and must pull — once. If that pull is not saved,
// every later alternation pulls again, which is exactly what the implicit
// `FROM` fetch did for as long as it bypassed this function.
func TestEnsureImageCachesWhatItPulls(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	const img = "docker.io/library/debian:bookworm-slim"

	out, rec, err := runLibPodman(t, `ensure_image `+img+` arm64`,
		"IMAGE_CACHE="+cache, "STUB_ARCH=amd64", "STUB_PULL_ARCH=arm64")
	if err != nil {
		t.Fatalf("ensure_image: %v\n%s", err, out)
	}

	if pulls := stubRecord(t, rec, "pulls"); !strings.Contains(pulls, "linux/arm64") {
		t.Errorf("ensure_image did not pull the target platform; pulls = %q\n%s", pulls, out)
	}
	// The finished artifact, not the `podman save` call: save_image_cache
	// writes a per-PID temp file and renames it, so asserting on the call would
	// pass for a save that was never moved into place — and it is the file
	// under its final name that the next build looks for.
	tar := filepath.Join(cache, "debian-bookworm-slim-arm64.tar")
	if _, statErr := os.Stat(tar); statErr != nil {
		t.Errorf("ensure_image pulled an arm64 base and left no arm64 tar (%v), so the next "+
			"switch of TARGET pulls it again — which is the bug this exists to fix\n%s", statErr, out)
	}
}

// TestEnsureImageLeavesTheOtherArchsTarAlone pins the rule that makes
// alternating cheap in both directions: a wrong-arch image in storage is
// dropped, the tar keyed to the arch being asked for is not.
func TestEnsureImageLeavesTheOtherArchsTarAlone(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	const img = "docker.io/library/debian:bookworm-slim"
	amd := filepath.Join(cache, "debian-bookworm-slim-amd64.tar")
	writeFakeTar(t, amd)
	writeFakeTar(t, filepath.Join(cache, "debian-bookworm-slim-arm64.tar"))

	out, _, err := runLibPodman(t, `ensure_image `+img+` arm64`,
		"IMAGE_CACHE="+cache, "STUB_ARCH=amd64", "STUB_LOAD_ARCH=arm64")
	if err != nil {
		t.Fatalf("ensure_image: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(amd); statErr != nil {
		t.Errorf("the amd64 tar was removed while staging arm64 (%v); the next native build "+
			"would re-pull it, which is the loop this keying exists to break", statErr)
	}
}

// cachePodmanStub answers the four calls ensure_image makes and records the
// ones that cost bandwidth.
//
// `image inspect` reports STUB_ARCH, and after a load or pull reports the arch
// that operation brought in — without that, ensure_image's post-load
// verification would see the old value and treat every load as wrong-arch.
const cachePodmanStub = `#!/bin/bash
state="${STUB_RECORD}/arch"
[ -f "${state}" ] || printf '%s' "${STUB_ARCH:-}" > "${state}"
case "$1 $2" in
  "image exists")
    [ -s "${state}" ]
    ;;
  "image inspect")
    arch="$(cat "${state}")"
    if [ -z "${arch}" ]; then
      echo "Error: no such image" >&2
      exit 1
    fi
    echo "${arch}"
    ;;
  "load "*)
    echo "$3" >> "${STUB_RECORD}/loads"
    printf '%s' "${STUB_LOAD_ARCH:-}" > "${state}"
    ;;
  "pull "*)
    echo "$*" >> "${STUB_RECORD}/pulls"
    printf '%s' "${STUB_PULL_ARCH:-}" > "${state}"
    ;;
  "save "*)
    echo "$3" >> "${STUB_RECORD}/saves"
    : > "$3"
    ;;
  "rmi "*)
    : > "${state}"
    ;;
esac
`

// runLibPodman is runLib with the cache-aware stub: same sourcing of
// make/lib.sh, same cleared SUDO, a stub that models storage instead of tags.
func runLibPodman(t *testing.T, snippet string, env ...string) (string, string, error) {
	t.Helper()
	return runLibWithStub(t, cachePodmanStub, snippet, env...)
}

// stubRecord returns one of the stub's record files, or "" when the stub was
// never asked to do that thing.
func stubRecord(t *testing.T, recordDir, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(recordDir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read stub %s record: %v", name, err)
	}
	return string(body)
}

// writeFakeTar stands in for a cached image tar. Its contents never matter:
// ensure_image decides on the file's existence and on what podman says about
// the image afterwards, and podman here is the stub.
func writeFakeTar(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("not a real tar"), 0o600); err != nil {
		t.Fatalf("write fake tar %s: %v", path, err)
	}
}
