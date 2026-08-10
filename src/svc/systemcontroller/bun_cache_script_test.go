// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bun_cache_reclaim (make/lib.sh) repairs the shared bun cache after a rootful
// build has written to it: it chowns the tree back to the invoking user and
// deletes entries whose absolute lookup symlink no longer resolves.
//
// The pruning half is what these tests cover, because it is the half that can be
// exercised without root and the half that decides whether the cache ever warms.
// Bun records a package as <cache>/<name>/<version> -> <cache>/<name>@<version>,
// an ABSOLUTE link, so an entry written while the cache lived at some other path
// -- the historical /bun-cache mount, or a checkout since moved -- points at
// nothing. Bun reads that as a miss and cannot replace the symlink, so it stays
// a miss on every subsequent run: the cache is permanently cold in exactly the
// way that looks like an ordinary slow network.
//
// The chown half is deliberately NOT exercised. It needs root over a directory
// the test does not own, and the test rules forbid a test that could touch the
// host that way; SUDO is cleared here for the same reason it is cleared in
// dev_dns_script_test.go, so no privileged call is reachable at all. What
// remains -- does the reclaim delete exactly the unresolvable entries -- is the
// behavior every bug in this path has been.
//
// There is no integration counterpart. The integration binary runs INSIDE the
// test container, where the repo is not mounted and make/lib.sh does not exist,
// so a host-side make script can only be tested by a host-side test (the same
// reasoning as src/rolodex/dev_restore_dns_test.go). This unit pass is the
// complete coverage.

// runBunCacheReclaim sources make/lib.sh with BUN_CACHE pointed at cacheDir and
// SUDO cleared, then calls bun_cache_reclaim.
func runBunCacheReclaim(t *testing.T, cacheDir string) {
	t.Helper()

	root := repoRoot(t)
	// SUDO="" so every privileged call in the function degrades to running the
	// command directly against a directory the test owns. Nothing here can reach
	// the machine's own caches.
	script := `set -e; SUDO=""; BUN_CACHE="$1"; . make/lib.sh; SUDO=""; bun_cache_reclaim`

	// The reclaim is local filesystem work, so it has no business taking this
	// long; the bound exists so a wedged bash cannot hang the whole run.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", script, "bash", cacheDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "BUN_CACHE="+cacheDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun_cache_reclaim: %v\n%s", err, out)
	}
}

// bunCacheEntry writes a package directory and its lookup symlink, mimicking
// bun's on-disk layout. target is what the symlink points at verbatim, so a test
// can plant the exact shape a wrong mount path produces.
func bunCacheEntry(t *testing.T, cacheDir, name, version, target string) string {
	t.Helper()

	pkgDir := filepath.Join(cacheDir, name+"@"+version+"@@@1")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte("//\n"), 0o644); err != nil {
		t.Fatalf("write index.js: %v", err)
	}
	nameDir := filepath.Join(cacheDir, name)
	if err := os.MkdirAll(nameDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", nameDir, err)
	}
	link := filepath.Join(nameDir, version+"@@@1")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}
	return link
}

// TestBunCacheReclaimPrunesUnresolvableEntries plants the exact wreckage the
// fixed /bun-cache mount used to leave -- a lookup symlink naming a path that
// does not exist on this machine -- and asserts the reclaim removes it while
// leaving a correctly-pathed entry alone.
func TestBunCacheReclaimPrunesUnresolvableEntries(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()

	// What a build mounting the cache at /bun-cache wrote. /bun-cache exists
	// only inside that container, so on the host this link resolves to nothing.
	stale := bunCacheEntry(t, cacheDir, "react", "19.2.4", "/bun-cache/react@19.2.4@@@1")
	// What a build mounting the cache at its own path writes.
	good := bunCacheEntry(t, cacheDir, "clsx", "2.1.1", filepath.Join(cacheDir, "clsx@2.1.1@@@1"))

	runBunCacheReclaim(t, cacheDir)

	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Errorf("unresolvable entry %s survived the reclaim (err=%v); bun will read "+
			"it as a miss on every run and cannot replace it, so the package is "+
			"re-downloaded from npmjs forever", stale, err)
	}
	if _, err := os.Lstat(good); err != nil {
		t.Errorf("resolvable entry %s was deleted: %v", good, err)
	}

	// The package payload behind the pruned link is still worth keeping: bun
	// re-extracts from it rather than re-fetching the tarball.
	if _, err := os.Stat(filepath.Join(cacheDir, "react@19.2.4@@@1", "index.js")); err != nil {
		t.Errorf("reclaim deleted the extracted package behind the stale link: %v", err)
	}
}

// TestBunCacheReclaimKeepsSymlinkChains guards the reason bun_cache_reclaim uses
// `-type l ! -exec test -e` rather than the shorter `-xtype l`: -xtype matches a
// symlink whose target is itself a symlink, which resolves fine and is not ours
// to delete.
func TestBunCacheReclaimKeepsSymlinkChains(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	final := bunCacheEntry(t, cacheDir, "react", "19.2.4", filepath.Join(cacheDir, "react@19.2.4@@@1"))

	chain := filepath.Join(cacheDir, "react", "19.x@@@1")
	if err := os.Symlink(final, chain); err != nil {
		t.Fatalf("symlink chain: %v", err)
	}

	runBunCacheReclaim(t, cacheDir)

	if _, err := os.Lstat(chain); err != nil {
		t.Errorf("symlink-to-symlink %s was deleted: %v; it resolves, so it is a "+
			"cache hit the reclaim just threw away", chain, err)
	}
}

// TestBunCacheReclaimToleratesAMissingCache asserts the reclaim is safe to run
// from an EXIT trap on a build that failed before the cache was created. A
// reclaim that failed there would turn a build error into a confusing second
// error and mask the first.
func TestBunCacheReclaimToleratesAMissingCache(t *testing.T) {
	t.Parallel()

	runBunCacheReclaim(t, filepath.Join(t.TempDir(), "never-created"))
}

// TestBunInstallWarnsOnUnresolvableCache asserts bun_install names the condition
// instead of silently paying for it. Only a build can repair the cache
// (bun_cache_reclaim needs root), so the host-side path has to say so -- an
// unresolvable cache and a slow npmjs are otherwise the same experience.
func TestBunInstallWarnsOnUnresolvableCache(t *testing.T) {
	t.Parallel()

	lib := readRepoFile(t, "make/lib.sh")
	start := strings.Index(lib, "bun_install() {")
	if start < 0 {
		t.Fatal("make/lib.sh: bun_install not found")
	}
	body := lib[start:]
	if idx := strings.Index(body, "\n}"); idx >= 0 {
		body = body[:idx]
	}
	if !strings.Contains(body, "unresolvable") {
		t.Error("make/lib.sh: bun_install does not check the cache for unresolvable " +
			"entries; a cold-forever cache is indistinguishable from a slow network " +
			"and went unnoticed for weeks the last time")
	}
	if !strings.Contains(body, "clean-bun-cache") {
		t.Error("make/lib.sh: bun_install's cache warning should name the repair " +
			"('make clean-bun-cache'), since the host cannot fix a root-owned cache")
	}
}
