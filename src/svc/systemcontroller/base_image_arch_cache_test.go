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
// The other direction cost a second round to find. A LOCAL build is native, so
// its base cannot be the wrong architecture by construction — except that a
// cross build before it left the target arch under the name, and the only thing
// that put the host's back was `load-base`, reached through the
// $(STATE_DIR)/.images-pulled prerequisite. That is a stamp file: make writes it
// once and thereafter considers it up to date, so the restore ran before the
// first cross build and never again. `podman build --pull=never` then built the
// test harness FROM an aarch64 debian on an x86_64 box. Every arm stages its own
// bases now, local ones included, which is why armContainerfiles covers both.
//
// Host-side, like every other test over these scripts: the integration binary
// runs inside the test container, where the repo is not mounted and
// make/lib.sh does not exist. The shell runs against a stub podman with SUDO
// cleared, so nothing here reaches a registry, the image store, or root.

// armContainerfiles maps each build arm to the Containerfile it builds.
//
// Local arms are in here with the release ones deliberately. A local build is
// native, so its base "cannot" be the wrong architecture — except that a cross
// build before it repointed the name, and the restore that used to fix that ran
// behind a stamp file which is only ever written once (see stage_runtime_bases
// in make/build.sh). Both kinds of arm need the same call for the same reason,
// so both are checked the same way.
var armContainerfiles = map[string]string{
	"release":         "Containerfile",
	"release-ui":      "Containerfile.ui",
	"release-nc":      "Containerfile.networkcontroller",
	"release-ingress": "Containerfile.ingress",
	"release-gfeh":    "Containerfile.gfeh",
	"release-proton":  "Containerfile.proton",

	"production":     "Containerfile",
	"dev-base":       "Containerfile",
	"test":           "integration/testdata/Containerfile.systemd",
	"dev":            "integration/testdata/Containerfile.dev",
	"ui-integration": "integration/testdata/Containerfile.ui-integration",
	"ui-local":       "Containerfile.ui",
	"ingress-local":  "Containerfile.ingress",
	"gfeh-local":     "Containerfile.gfeh",
	// nc-local builds an inline Containerfile rather than a file on disk, so it
	// has nothing to parse; TestNCLocalStagesItsInlineBase covers it.
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
		// `FROM ${TOWN_OS_IMAGE}` names an image this repo just built, passed in
		// as a build-arg. It is not an upstream base and there is nothing to
		// stage: it is already in storage or the build has bigger problems.
		if platform != "" || stages[ref] || seen[ref] || strings.Contains(ref, "$") {
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

// TestBuildArmsStageEveryRuntimeBase is the guard that survives a new
// Containerfile stage. The expected set is derived FROM THE CONTAINERFILE, so
// adding a runtime base without staging it fails here rather than on the next
// person's bandwidth.
func TestBuildArmsStageEveryRuntimeBase(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)
	for arm, containerfile := range armContainerfiles {
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

// TestNCLocalStagesItsInlineBase covers the one arm with no Containerfile to
// parse: nc-local writes its Containerfile inline, FROM alpine, and builds it
// with --pull=never. It has always staged that base — it was the one arm that
// did, through a bare ensure_image call — and it must keep doing so through the
// same function as everything else, or it becomes the exception that quietly
// stops matching the rule the other arms are held to.
func TestNCLocalStagesItsInlineBase(t *testing.T) {
	t.Parallel()

	arm := caseArm(t, readRepoFile(t, buildScript), "nc-local")
	const base = "docker.io/library/alpine:latest"

	if !strings.Contains(arm, "FROM "+base) {
		t.Fatalf("nc-local no longer builds FROM %s; this test is pinned to the wrong base", base)
	}
	if !strings.Contains(arm, "stage_runtime_bases "+base) {
		t.Errorf("make/build.sh: the nc-local arm does not stage %s through stage_runtime_bases, "+
			"so a cross build that repointed that name leaves it building the wrong architecture "+
			"under --pull=never", base)
	}
}

// crossBuildableContainerfiles are the Containerfiles a cross TARGET can build.
//
// Containerfile.proton is deliberately absent: it refuses any TARGET but
// x86_64 (GE-Proton is x86_64 Wine), so its bare-FROM base is never wanted at
// another architecture. The native-only test and dev images are absent for the
// same reason from the other direction — require_native_target refuses a cross
// TARGET for them outright.
var crossBuildableContainerfiles = []string{
	"Containerfile",
	"Containerfile.ui",
	"Containerfile.ingress",
	"Containerfile.networkcontroller",
	"Containerfile.gfeh",
}

// TestBaseImagesRuntimeMatchesTheContainerfiles keeps the Makefile's
// BASE_IMAGES_RUNTIME list honest against the files it claims to describe.
//
// load-base stages that subset at the BUILD arch and everything else at the
// host's. Getting the membership wrong is quiet in both directions: a runtime
// base left out is staged at the host arch on a cross build and has to be
// replaced by the build arm moments later (the round trip that made
// `podman image inspect` report the host arch mid-cross-build), and a toolchain
// base wrongly included is staged at the target arch when the $BUILDPLATFORM
// stage that uses it needs the host's.
func TestBaseImagesRuntimeMatchesTheContainerfiles(t *testing.T) {
	t.Parallel()

	makefile := readRepoFile(t, "Makefile")
	all := makeListVar(t, makefile, "BASE_IMAGES")
	runtime := makeListVar(t, makefile, "BASE_IMAGES_RUNTIME")

	inAll := map[string]bool{}
	for _, img := range all {
		inAll[img] = true
	}
	inRuntime := map[string]bool{}
	for _, img := range runtime {
		if !inAll[img] {
			t.Errorf("BASE_IMAGES_RUNTIME lists %s, which is not in BASE_IMAGES; load-base only "+
				"iterates BASE_IMAGES, so the entry does nothing", img)
		}
		inRuntime[img] = true
	}

	// Every bare-FROM base of a cross-buildable Containerfile that load-base
	// stages at all has to follow TARGET.
	want := map[string]bool{}
	for _, cf := range crossBuildableContainerfiles {
		for _, base := range runtimeBases(t, cf) {
			if !inAll[base] {
				continue // staged by the build arm alone (alpine); load-base never sees it
			}
			want[base] = true
			if !inRuntime[base] {
				t.Errorf("%s resolves %s at the target platform, but BASE_IMAGES_RUNTIME omits it: "+
					"load-base stages it at the HOST arch and a cross build has to undo that", cf, base)
			}
		}
	}
	for _, img := range runtime {
		if inAll[img] && !want[img] {
			t.Errorf("BASE_IMAGES_RUNTIME lists %s, but no cross-buildable Containerfile names it "+
				"with a bare FROM; staging it at the target arch denies the host arch to whatever "+
				"$BUILDPLATFORM stage uses it", img)
		}
	}
}

// makeListVar reads a `NAME := a b c` assignment out of a Makefile.
func makeListVar(t *testing.T, makefile, name string) []string {
	t.Helper()

	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:?=\s*(.*)$`)
	m := re.FindStringSubmatch(makefile)
	if m == nil {
		t.Fatalf("Makefile has no %s assignment", name)
	}
	fields := strings.Fields(m[1])
	if len(fields) == 0 {
		t.Fatalf("Makefile: %s is empty", name)
	}
	return fields
}

// TestLoadBaseStagesRuntimeBasesAtTheBuildArch pins the mechanism itself: the
// loop has to ask ensure_image for build_oci_arch on the runtime subset. A
// load-base that passes no architecture defaults to the host's for everything,
// which is where this started.
func TestLoadBaseStagesRuntimeBasesAtTheBuildArch(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, "make/images.sh")
	arm := caseArm(t, script, "load-base")
	if !strings.Contains(arm, "BASE_IMAGES_RUNTIME") {
		t.Error("make/images.sh: load-base does not consult BASE_IMAGES_RUNTIME, so every base is " +
			"staged at the host arch and a cross build's prerequisites undo its own staging")
	}
	if !strings.Contains(arm, "build_oci_arch") {
		t.Error("make/images.sh: load-base never passes build_oci_arch to ensure_image; without an " +
			"architecture argument ensure_image defaults to the host's")
	}
}
