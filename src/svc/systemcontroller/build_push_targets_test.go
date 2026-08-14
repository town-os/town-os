// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The release pipeline keeps the same image set in five places in make/build.sh
// — push-rc, manifest-rc, push-release, manifest-release, push-tag — and
// nothing but agreement between them makes a release complete. Every drift so
// far has been an omission, and every omission has been silent:
//
//   - gfeh was in both manifest lists and in neither bulk push. `manifest-rc`
//     therefore assembled quay.io/town/gfeh:rc.latest out of whatever per-arch
//     tags a hand-run `make push-gfeh-rc` had left behind — or failed on a tag
//     that was never pushed at all.
//   - ingress was in manifest-rc and push-rc but missing from push-release, so
//     `latest` and `release.<date>` for the ingress image stayed pinned to
//     whichever single-arch tag went up last. On the other architecture that is
//     an `exec format error` at boot, which reads as a broken image rather than
//     as a release that forgot to push one.
//   - push-tag pushed four of six images under a tag whose entire purpose is
//     that every quay.io/town/* image carries it.
//
// None of that fails a build, a lint, or any other test: the push succeeds, the
// manifest assembles, and the box pulls a stale or absent image at boot. So the
// lists are compared here, statically — no podman, no network, no registry.
//
// Same shape and same reasoning as containerfile_bun_cache_test.go: the failure
// being guarded is invisible at the time it is made.

// buildScript is the release pipeline, relative to the repo root.
const buildScript = "make/build.sh"

// releaseImageVars are the image variables a complete release covers.
//
// Proton is deliberately absent: it is gated behind PROTON_ENABLED and appears
// only inside conditionals, so it is not part of the unconditional set every
// target must carry.
var releaseImageVars = []string{
	"RELEASE_IMAGE",
	"RELEASE_UI_IMAGE",
	"RELEASE_NC_IMAGE",
	"RELEASE_INGRESS_IMAGE",
	"RELEASE_GFEH_IMAGE",
}

// caseArm returns the body of one `case` arm from build.sh: everything between
// the `  <name>)` line and the `    ;;` that closes it.
//
// Sliced rather than parsed because the arms are flat — no nested case, and the
// only `;;` inside one is its own terminator. A parser here would be more code
// than the thing it reads.
func caseArm(t *testing.T, script, name string) string {
	t.Helper()
	_, rest, found := strings.Cut(script, "\n  "+name+")\n")
	if !found {
		t.Fatalf("%s has no %q case arm", buildScript, name)
	}
	body, _, terminated := strings.Cut(rest, "\n    ;;")
	if !terminated {
		t.Fatalf("%s: %q case arm is not terminated", buildScript, name)
	}
	return body
}

// pushedImages returns the image variables the arm runs `podman push` on, and
// imagesIn returns every image variable the arm mentions at all.
func pushedImages(arm string) []string {
	re := regexp.MustCompile(`podman push "\$\{(RELEASE_[A-Z_]+)\}`)
	return uniqueSorted(re.FindAllStringSubmatch(arm, -1))
}

func manifestImages(arm string) []string {
	re := regexp.MustCompile(`\$\{(RELEASE_[A-Z_]+)\}`)
	return uniqueSorted(re.FindAllStringSubmatch(arm, -1))
}

func uniqueSorted(matches [][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// TestBulkPushTargetsCoverEveryReleaseImage asserts push-rc, push-release and
// push-tag each push every image in the release set. An image built by
// release-build and never pushed is the whole bug: the make prerequisites
// already build gfeh, so it sat in the local store, correct and unshipped.
func TestBulkPushTargetsCoverEveryReleaseImage(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)
	for _, target := range []string{"push-rc", "push-release", "push-tag"} {
		pushed := pushedImages(caseArm(t, script, target))
		for _, want := range releaseImageVars {
			if !slices.Contains(pushed, want) {
				t.Errorf("%s does not push %s; it is built by release-build and would ship stale or absent", target, want)
			}
		}
	}
}

// TestManifestTargetsMatchTheirPushTargets asserts each manifest arm assembles
// exactly the images its push arm pushed per-arch.
//
// Both directions are failures, and they fail differently. A manifest for an
// image nobody pushed resolves to missing or stale per-arch tags; a pushed
// image with no manifest leaves its plain name (`rc.latest`, `latest`) as
// whichever single-arch tag went up last, which is an `exec format error` on
// the other architecture.
func TestManifestTargetsMatchTheirPushTargets(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)
	for _, pair := range []struct{ push, manifest string }{
		{"push-rc", "manifest-rc"},
		{"push-release", "manifest-release"},
	} {
		pushed := pushedImages(caseArm(t, script, pair.push))
		assembled := manifestImages(caseArm(t, script, pair.manifest))

		for _, img := range pushed {
			if img == "RELEASE_PROTON_IMAGE" {
				continue // conditional on PROTON_ENABLED in both arms
			}
			if !slices.Contains(assembled, img) {
				t.Errorf("%s pushes %s per-arch but %s never assembles its manifest; the plain tag stays single-arch", pair.push, img, pair.manifest)
			}
		}
		for _, img := range assembled {
			if img == "RELEASE_PROTON_IMAGE" {
				continue
			}
			if !slices.Contains(pushed, img) {
				t.Errorf("%s assembles a manifest for %s that %s never pushes per-arch", pair.manifest, img, pair.push)
			}
		}
	}
}

// TestPushTargetsHaveBuildPrerequisites asserts every image a bulk push arm
// pushes is built by that target's make prerequisites.
//
// A push target that tags an image nothing built does not fail cleanly: podman
// tags whatever stale copy the local store happens to hold from an earlier run,
// and pushes that. On a fresh checkout it fails; on a developer's box it
// succeeds and ships the wrong bits, which is the worse of the two.
func TestPushTargetsHaveBuildPrerequisites(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)
	// Prerequisites live in two files: push-rc/push-release in the Makefile,
	// push-tag in make/include.mk.
	prereqs := map[string]string{
		"push-rc":      readRepoFile(t, "Makefile"),
		"push-release": readRepoFile(t, "Makefile"),
		"push-tag":     readRepoFile(t, "make/include.mk"),
	}
	// The make target that builds each image.
	builders := map[string]string{
		"RELEASE_IMAGE":         "release-image",
		"RELEASE_UI_IMAGE":      "release-ui-image",
		"RELEASE_NC_IMAGE":      "release-nc-image",
		"RELEASE_INGRESS_IMAGE": "release-ingress-image",
		"RELEASE_GFEH_IMAGE":    "release-gfeh-image",
	}

	for target, makefile := range prereqs {
		line := prerequisiteLine(makefile, target)
		if line == "" {
			t.Errorf("no prerequisite line for %s", target)
			continue
		}
		for _, img := range pushedImages(caseArm(t, script, target)) {
			builder, ok := builders[img]
			if !ok {
				continue // proton, gated separately
			}
			// push-rc and push-release reach their builders through
			// release-build; either naming is fine.
			if !strings.Contains(line, builder) && !strings.Contains(line, "release-build") {
				t.Errorf("%s pushes %s but its prerequisites (%q) do not build it via %s", target, img, line, builder)
			}
		}
	}
}

// localArms are the build arms that produce images for the test and dev
// harnesses, and releaseArms the ones that produce images for a push.
var localArms = []string{"ui-local", "nc-local", "ingress-local", "gfeh-local"}

var releaseArms = []string{"release", "release-ui", "release-nc", "release-ingress", "release-gfeh"}

// TestBuildArmsKeepLocalAndReleaseImagesApart asserts the CLAUDE.md rule that
// test and dev build `localhost/` images while push targets always build a fresh
// release image.
//
// A push arm that re-tagged a local image would ship bits built for the harness
// — per-instance tags, --pull=never bases, host arch only, never cross-built —
// under a release name. On a fresh checkout that fails; on a developer's box it
// succeeds, which is the worse of the two and the reason this is a test rather
// than a convention.
func TestBuildArmsKeepLocalAndReleaseImagesApart(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, buildScript)

	// A local arm may only build a local image variable, and a release arm may
	// only build a RELEASE_* one. The -t flag is what decides which.
	//
	// Both spellings of -t count. Release arms build through staged_ref
	// (`-t "$(staged_ref "${RELEASE_X_IMAGE}")"`, the arch-suffixed staging
	// name) and local arms name their variable directly; a regex that read only
	// the direct form would match nothing in a release arm and pass this test by
	// checking nothing at all.
	tagged := regexp.MustCompile(`-t "(?:\$\(staged_ref ")?\$\{([A-Z_]+)\}`)
	for _, arm := range localArms {
		for _, m := range tagged.FindAllStringSubmatch(caseArm(t, script, arm), -1) {
			if strings.HasPrefix(m[1], "RELEASE_") {
				t.Errorf("local arm %s builds %s; test and dev images must be localhost/ images", arm, m[1])
			}
		}
	}
	for _, arm := range releaseArms {
		for _, m := range tagged.FindAllStringSubmatch(caseArm(t, script, arm), -1) {
			if !strings.HasPrefix(m[1], "RELEASE_") {
				t.Errorf("release arm %s builds %s, which is not a RELEASE_* image", arm, m[1])
			}
		}
	}

	// No push arm may touch a localhost image, by any route: building one,
	// tagging from one, or naming one at all.
	for _, target := range []string{"push-rc", "push-release", "push-tag", "push-ui-rc", "push-ui-release", "push-nc-rc", "push-nc-release", "push-ingress-rc", "push-ingress-release", "push-gfeh-rc", "push-gfeh-release"} {
		body := caseArm(t, script, target)
		if strings.Contains(body, "localhost/") {
			t.Errorf("%s references a localhost/ image; push targets must build and push release images only", target)
		}
		for _, m := range tagged.FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], "RELEASE_") {
				t.Errorf("%s builds %s, which is not a RELEASE_* image", target, m[1])
			}
		}
	}
}

// TestLocalImageVarsAreLocalhostScoped asserts the Makefile keeps the harness
// image variables on localhost/ and per-instance, and the release ones on
// quay.io/town. The build arms above are checked by variable name, so this is
// what stops the names themselves from drifting underneath them.
func TestLocalImageVarsAreLocalhostScoped(t *testing.T) {
	t.Parallel()

	makefile := readRepoFile(t, "Makefile")
	for _, v := range []string{"UI_IMAGE", "NC_IMAGE", "INGRESS_IMAGE", "GFEH_IMAGE"} {
		re := regexp.MustCompile(`(?m)^` + v + ` \?= (.*)$`)
		m := re.FindStringSubmatch(makefile)
		if m == nil {
			t.Errorf("Makefile has no %s ?= line", v)
			continue
		}
		if !strings.HasPrefix(m[1], "localhost/") {
			t.Errorf("%s is %q; harness images must be localhost/ so nothing tries to pull them", v, m[1])
		}
		if !strings.Contains(m[1], "$(INSTANCE_ID)") {
			t.Errorf("%s is %q; harness images must be per-instance so concurrent runs cannot collide", v, m[1])
		}
	}
	for _, v := range releaseImageVars {
		re := regexp.MustCompile(`(?m)^` + v + `\s*:= (.*)$`)
		m := re.FindStringSubmatch(makefile)
		if m == nil {
			t.Errorf("Makefile has no %s := line", v)
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(m[1]), "quay.io/town/") {
			t.Errorf("%s is %q; release images must be quay.io/town/*", v, m[1])
		}
	}
}

// prerequisiteLine returns the first `<target>: ...` rule line for target,
// skipping any line where the name appears as a prerequisite rather than as the
// rule's own target.
func prerequisiteLine(makefile, target string) string {
	for line := range strings.SplitSeq(makefile, "\n") {
		if strings.HasPrefix(line, target+":") && !strings.HasPrefix(line, target+"::") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
