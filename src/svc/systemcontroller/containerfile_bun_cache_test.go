// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The shared bun cache (BUN_CACHE, .cache/bun) is used from two sides: host-side
// `bun install` via bun_install's --cache-dir, and the container builds that
// mount it. Bun resolves a package through an ABSOLUTE symlink --
//
//	<cache>/react/19.2.4@@@1 -> <cache>/react@19.2.4@@@1
//
// -- so the cache directory's own path is written into every entry in it, and a
// cache mounted under a different path is a cache of dangling links.
//
// That is not a hypothetical. The container builds mounted BUN_CACHE at a fixed
// /bun-cache, so every entry they recorded named a path with no existence on the
// host. Host-side bun missed on all ~770 packages of ui/bun.lock and re-fetched
// the lockfile from npmjs on every run; and because rootful podman left the
// per-package directories root:root 0755, host bun could not write a version
// symlink back into one, so it could not record what it had just downloaded
// either. The cache was write-only as seen from the host and never warmed: a
// full network install on every `make dev`, indistinguishable from a slow link.
//
// Two invariants keep that from coming back, and both are asserted here:
//
//  1. path agreement -- the Containerfiles take the cache path as the
//     BUN_CACHE_DIR build-arg, and every build that runs `bun install` mounts
//     BUN_CACHE at BUN_CACHE and forwards it (make/lib.sh's BUN_BUILD_ARGS).
//  2. ownership handback -- every such target registers bun_cache_reclaim, which
//     chowns the cache back to the invoking user and prunes entries whose
//     absolute symlink no longer resolves.
//
// Static analysis, like containerfile_cabundle_test.go: no build, no network, no
// podman. The failure being guarded is a silent one -- a wrong mount path costs
// minutes per invocation and reports nothing -- so it has to be caught by
// reading the files rather than by noticing the build got slow.

// bunContainerfiles are the Containerfiles with a `bun install` in them. A file
// added here without the ARG/ENV pair below fails the test.
var bunContainerfiles = []string{
	"Containerfile",
	"Containerfile.ui",
	"integration/testdata/Containerfile.ui-integration",
}

// TestBunContainerfilesTakeCachePathAsBuildArg asserts every bun stage reads its
// cache location from the BUN_CACHE_DIR build-arg rather than hardcoding one,
// and that the ARG precedes the install it has to affect.
func TestBunContainerfilesTakeCachePathAsBuildArg(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	for _, name := range bunContainerfiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			text := string(body)

			// A literal cache path defeats the whole arrangement: it is what
			// the host could not resolve.
			if strings.Contains(text, "BUN_INSTALL_CACHE_DIR=/bun-cache") {
				t.Errorf("%s pins BUN_INSTALL_CACHE_DIR to a fixed /bun-cache; "+
					"bun records absolute paths in its cache, so the host-side "+
					"bun sharing that directory sees only dangling symlinks. "+
					"Take the path as ARG BUN_CACHE_DIR and let BUN_BUILD_ARGS "+
					"supply it", name)
			}

			argLine := indexOfLine(text, func(l string) bool {
				return strings.HasPrefix(l, "ARG BUN_CACHE_DIR=")
			})
			envLine := indexOfLine(text, func(l string) bool {
				return l == "ENV BUN_INSTALL_CACHE_DIR=${BUN_CACHE_DIR}"
			})
			installLine := indexOfLine(text, func(l string) bool {
				return strings.HasPrefix(l, "RUN bun install")
			})

			if argLine < 0 {
				t.Errorf("%s runs bun install but declares no `ARG BUN_CACHE_DIR=`", name)
			}
			if envLine < 0 {
				t.Errorf("%s does not set `ENV BUN_INSTALL_CACHE_DIR=${BUN_CACHE_DIR}`", name)
			}
			if installLine < 0 {
				t.Fatalf("%s is listed as a bun Containerfile but has no `RUN bun install`", name)
			}
			// An ARG declared after the install it is meant to configure is a
			// no-op, and looks correct in a diff.
			if argLine >= 0 && argLine > installLine {
				t.Errorf("%s declares ARG BUN_CACHE_DIR (line %d) after RUN bun install (line %d); "+
					"the install would use the image default", name, argLine+1, installLine+1)
			}
			if envLine >= 0 && envLine > installLine {
				t.Errorf("%s sets BUN_INSTALL_CACHE_DIR (line %d) after RUN bun install (line %d)",
					name, envLine+1, installLine+1)
			}

			// The default keeps a hand-run `podman build` with no --build-arg
			// working, but must never be what the make pipeline relies on.
			if argLine >= 0 && !strings.Contains(text, "ARG BUN_CACHE_DIR=/bun-cache") {
				t.Errorf("%s: ARG BUN_CACHE_DIR should default to /bun-cache for a "+
					"build run by hand without --build-arg", name)
			}
		})
	}
}

// TestBunBuildArgsMountCacheAtItsOwnPath asserts make/lib.sh mounts BUN_CACHE at
// BUN_CACHE -- the same path on both sides -- and forwards it as the build-arg
// the Containerfiles read.
func TestBunBuildArgsMountCacheAtItsOwnPath(t *testing.T) {
	t.Parallel()

	lib := readRepoFile(t, "make/lib.sh")

	if !strings.Contains(lib, `--volume "${BUN_CACHE}:${BUN_CACHE}:z"`) {
		t.Error(`make/lib.sh: BUN_BUILD_ARGS must mount the cache at its own host path ` +
			`(--volume "${BUN_CACHE}:${BUN_CACHE}:z"). Bun's cache resolves through ` +
			`absolute symlinks, so a cache mounted anywhere else is a cache of ` +
			`dangling links for whichever side did not write it`)
	}
	if !strings.Contains(lib, `--build-arg "BUN_CACHE_DIR=${BUN_CACHE}"`) {
		t.Error(`make/lib.sh: BUN_BUILD_ARGS must forward the mount path as ` +
			`--build-arg "BUN_CACHE_DIR=${BUN_CACHE}" so the Containerfile agrees ` +
			`with where the cache actually landed`)
	}
	if !strings.Contains(lib, "bun_cache_reclaim()") {
		t.Error("make/lib.sh: bun_cache_reclaim is missing; a rootful build leaves " +
			"the cache root-owned and host-side bun can then never write to it")
	}
}

// buildBunTargets extracts, from make/build.sh, the case labels of every target
// that passes BUN_BUILD_ARGS to podman build.
//
// Each `case` arm here is a target name followed by `)`; the arms that mount the
// cache are found by walking forward from each label to the next one and looking
// for the expansion.
func buildBunTargets(t *testing.T, script string) []string {
	t.Helper()

	label := regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*)\)$`)
	locs := label.FindAllStringSubmatchIndex(script, -1)

	var targets []string
	for i, loc := range locs {
		end := len(script)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if strings.Contains(script[loc[1]:end], `"${BUN_BUILD_ARGS[@]}"`) {
			targets = append(targets, script[loc[2]:loc[3]])
		}
	}
	sort.Strings(targets)
	return targets
}

// TestEveryBunBuildReclaimsTheCache asserts that the set of build.sh targets
// mounting the bun cache is exactly the set registering bun_cache_reclaim.
//
// The two lists drift silently and in the expensive direction: a new target that
// mounts the cache without the reclaim leaves it root-owned, and the cost lands
// on the NEXT `make dev` -- a full re-download, in a different command, with
// nothing pointing back at the build that caused it.
func TestEveryBunBuildReclaimsTheCache(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, "make/build.sh")

	if !strings.Contains(script, "trap bun_cache_reclaim EXIT") {
		t.Fatal("make/build.sh does not register `trap bun_cache_reclaim EXIT`; " +
			"a rootful build leaves .cache/bun owned by root and host-side " +
			"bun_install can no longer record anything in it")
	}

	mounts := buildBunTargets(t, script)
	if len(mounts) == 0 {
		t.Fatal("make/build.sh: found no targets passing \"${BUN_BUILD_ARGS[@]}\"; " +
			"either the mount was removed or this test's parser is stale")
	}

	// The arm holding the trap lists its targets `a | b | c)`. Read them back
	// rather than restating them, so the test asserts agreement between the two
	// lists instead of pinning a third copy of the same set.
	trapArm := regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*(?: \| [a-z][a-z0-9-]*)*)\)\n\s*trap bun_cache_reclaim EXIT`)
	m := trapArm.FindStringSubmatch(script)
	if m == nil {
		t.Fatal("make/build.sh: could not read the case arm guarding " +
			"`trap bun_cache_reclaim EXIT`")
	}
	var reclaims []string
	for name := range strings.SplitSeq(m[1], "|") {
		reclaims = append(reclaims, strings.TrimSpace(name))
	}
	sort.Strings(reclaims)

	for _, target := range mounts {
		if !slices.Contains(reclaims, target) {
			t.Errorf("make/build.sh target %q mounts the bun cache but does not "+
				"register bun_cache_reclaim: it will leave .cache/bun root-owned "+
				"and cost the next `make dev` a full npmjs install", target)
		}
	}
	for _, target := range reclaims {
		if !slices.Contains(mounts, target) {
			t.Errorf("make/build.sh target %q registers bun_cache_reclaim but does "+
				"not mount the bun cache; drop it from the trap arm", target)
		}
	}
}

// TestNoFixedBunCacheMountRemains guards the specific regression: a
// `:/bun-cache` volume mount anywhere in the make scripts.
func TestNoFixedBunCacheMountRemains(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "make"))
	if err != nil {
		t.Fatalf("read make/: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, "make", entry.Name()))
		if err != nil {
			t.Fatalf("read make/%s: %v", entry.Name(), err)
		}
		if strings.Contains(string(body), ":/bun-cache") {
			t.Errorf("make/%s mounts the bun cache at a fixed /bun-cache; mount it "+
				"at ${BUN_CACHE} on both sides via \"${BUN_BUILD_ARGS[@]}\"", entry.Name())
		}
	}
}

// indexOfLine returns the 0-based index of the first trimmed line satisfying
// match, or -1.
func indexOfLine(text string, match func(string) bool) int {
	for i, line := range strings.Split(text, "\n") {
		if match(strings.TrimSpace(line)) {
			return i
		}
	}
	return -1
}

// readRepoFile reads a repo-relative file or fails the test.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(body)
}
