// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/gfeh"
)

// publishedRegistry is a partition serving every view, with files published out
// of it.
func publishedRegistry(exposures ...gfeh.Exposure) (stubGfehRegistry, *gfeh.MockClient) {
	client := allViews("home", "")
	client.Exposures = exposures
	return stubGfehRegistry{clients: map[string]gfeh.Client{"home": client}}, client
}

func published(token, filename string) gfeh.Exposure {
	name := filename
	return gfeh.Exposure{Token: token, Path: "/" + filename, Filename: &name, Enabled: true}
}

// The published-files index lands under the http view's own name, not the
// partition's: it is the root of the name whose every other path is gfehd's.
func TestReconcileGfehPublicIndexesWritesTheHTTPViewRoot(t *testing.T) {
	base := t.TempDir()
	reg, _ := publishedRegistry(published("abc123", "q3.pdf"))

	written := reconcileGfehPublicIndexes(context.Background(), reg, base, homeSites(t))

	if _, ok := written["http.gfeh.home"]; !ok {
		t.Fatalf("the http view's index was not written: %v", written)
	}
	raw, err := os.ReadFile(filepath.Join(base, GfehIndexDirName, "http.gfeh.home", "index.html"))
	if err != nil {
		t.Fatalf("read published index: %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "q3.pdf") || !strings.Contains(out, `href="/f/abc123"`) {
		t.Errorf("the index does not list the published file:\n%s", out)
	}

	// And the pages container has to be able to resolve it, which is the
	// symlink — the content alone is a directory nothing roots on.
	target, err := os.Readlink(filepath.Join(base, PagesWebrootDir, "http.gfeh.home"))
	if err != nil {
		t.Fatalf("webroot symlink: %v", err)
	}
	if want := GfehIndexContainerDir + "/http.gfeh.home"; target != want {
		t.Errorf("symlink target = %q, want %q", target, want)
	}
}

// Only the http view. The other three answer their own APIs at their roots and
// have no published links to list.
func TestReconcileGfehPublicIndexesLeavesTheOtherViewsAlone(t *testing.T) {
	base := t.TempDir()
	reg, _ := publishedRegistry(published("abc123", "q3.pdf"))

	reconcileGfehPublicIndexes(context.Background(), reg, base, homeSites(t))

	for _, fqdn := range []string{"s3.gfeh.home", "drive.gfeh.home", "ipfs.gfeh.home", "smb.gfeh.home"} {
		if _, err := os.Stat(filepath.Join(base, GfehIndexDirName, fqdn)); !os.IsNotExist(err) {
			t.Errorf("an index was written for %s", fqdn)
		}
	}
}

// A route cannot be programmed before the bytes it serves exist. Routing "/" to
// the pages container for a name it has no webroot entry for replaces gfehd's
// 404 with caddy's, which is the same failure with a different logo.
func TestGfehPublicIndexBackendsOnlyForWrittenNames(t *testing.T) {
	httpSite := GfehSite{Network: "home", View: gfeh.ViewHTTP, FQDN: "http.gfeh.home", HTTP: true}

	if got := gfehPublicIndexBackends(httpSite, nil); got != nil {
		t.Errorf("a path backend was programmed for a name with no index: %+v", got)
	}

	got := gfehPublicIndexBackends(httpSite, map[string]struct{}{"http.gfeh.home": {}})
	if len(got) != 1 {
		t.Fatalf("got %d path backends, want 1", len(got))
	}
	if got[0].GetPath() != gfehPublicIndexPath {
		t.Errorf("path = %q, want %q", got[0].GetPath(), gfehPublicIndexPath)
	}
	if got[0].GetBackend() != pagesBackend() {
		t.Errorf("backend = %q, want the pages container", got[0].GetBackend())
	}
}

// The S3 view's root is gfehd's business, not ours.
func TestGfehPublicIndexBackendsIgnoresOtherViews(t *testing.T) {
	for _, view := range []string{gfeh.ViewS3, gfeh.ViewDrive, gfeh.ViewIPFS, gfeh.ViewSMB, gfeh.ViewIndex} {
		site := GfehSite{Network: "home", View: view, FQDN: view + ".gfeh.home", HTTP: true}
		if got := gfehPublicIndexBackends(site, map[string]struct{}{site.FQDN: {}}); got != nil {
			t.Errorf("view %q got a path backend: %+v", view, got)
		}
	}
}

// Both indexes live under one root, so a prune that only knew about partition
// indexes would delete every published-files page on the pass that wrote it.
func TestReconcileGfehIndexesKeepsBothIndexes(t *testing.T) {
	base := t.TempDir()
	reg, _ := publishedRegistry(published("abc123", "q3.pdf"))

	for range 2 {
		reconcileGfehIndexes(context.Background(), reg, base, homeSites(t))
	}

	for _, fqdn := range []string{"gfeh.home", "http.gfeh.home"} {
		if _, err := os.Stat(filepath.Join(base, GfehIndexDirName, fqdn, "index.html")); err != nil {
			t.Errorf("%s was pruned: %v", fqdn, err)
		}
		if _, err := os.Lstat(filepath.Join(base, PagesWebrootDir, fqdn)); err != nil {
			t.Errorf("%s lost its webroot entry: %v", fqdn, err)
		}
	}
}

// The pages reconcile sweeps every webroot entry it cannot account for, and it
// accounts for them from state Town OS owns rather than from a daemon's answer.
// Without the http view in that set the entry is deleted on the first pass and
// the index 404s until the ingress is next rebuilt.
func TestGfehIndexHostnamesCoversThePublishedFilesIndex(t *testing.T) {
	reg, _ := publishedRegistry()

	names := gfehIndexHostnames(context.Background(), reg, nil, "home")

	for _, want := range []string{"gfeh.home", "http.gfeh.home"} {
		if _, ok := names[want]; !ok {
			t.Errorf("%s is not protected from the pages prune: %v", want, names)
		}
	}
}

// A daemon being briefly unreachable is not evidence that somebody withdrew
// every file. Rewriting the page to "nothing published yet" would empty the
// index on every restart and refill it a pass later.
func TestReconcileGfehPublicIndexesKeepsThePageWhenTheDaemonIsDown(t *testing.T) {
	base := t.TempDir()
	reg, client := publishedRegistry(published("abc123", "q3.pdf"))
	sites := homeSites(t)

	reconcileGfehPublicIndexes(context.Background(), reg, base, sites)
	path := filepath.Join(base, GfehIndexDirName, "http.gfeh.home", "index.html")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first index: %v", err)
	}

	client.Errors["ListExposures"] = errGfehIndexTestDown
	written := reconcileGfehPublicIndexes(context.Background(), reg, base, sites)

	if _, ok := written["http.gfeh.home"]; ok {
		t.Error("a name whose exposures could not be read was reported as written")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index after the daemon went away: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the page was rewritten while the daemon was down:\n%s", string(after))
	}
}

// The same thing again, but through the composed path, because that is where it
// can actually go wrong: reconcileGfehIndexes prunes the root both pages share,
// and a valid set built from what this pass *rendered* rather than from the
// names the box *has* deletes the page of every partition whose daemon happens
// to be unreachable — undoing, from the other direction, the decision not to
// rewrite it.
func TestReconcileGfehIndexesDoesNotPruneAPageItDeclinedToRewrite(t *testing.T) {
	base := t.TempDir()
	reg, client := publishedRegistry(published("abc123", "q3.pdf"))
	sites := homeSites(t)

	reconcileGfehIndexes(context.Background(), reg, base, sites)
	path := filepath.Join(base, GfehIndexDirName, "http.gfeh.home", "index.html")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first index: %v", err)
	}

	client.Errors["ListExposures"] = errGfehIndexTestDown
	reconcileGfehIndexes(context.Background(), reg, base, sites)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the page was pruned while its daemon was unreachable: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the page changed while the daemon was down:\n%s", string(after))
	}
	if _, err := os.Lstat(filepath.Join(base, PagesWebrootDir, "http.gfeh.home")); err != nil {
		t.Errorf("the webroot entry was pruned: %v", err)
	}
}

// Withdrawing a link is the only thing that takes a file off this page, so the
// reconcile after a withdrawal has to actually rewrite it.
func TestReconcileGfehPublicIndexesFollowsAWithdrawal(t *testing.T) {
	base := t.TempDir()
	reg, client := publishedRegistry(published("abc123", "q3.pdf"), published("def456", "photo.jpg"))
	sites := homeSites(t)

	reconcileGfehPublicIndexes(context.Background(), reg, base, sites)

	if err := client.WithdrawExposure(context.Background(), "abc123"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	reconcileGfehPublicIndexes(context.Background(), reg, base, sites)

	raw, err := os.ReadFile(filepath.Join(base, GfehIndexDirName, "http.gfeh.home", "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	out := string(raw)
	if strings.Contains(out, "abc123") || strings.Contains(out, "q3.pdf") {
		t.Errorf("a withdrawn file is still listed:\n%s", out)
	}
	if !strings.Contains(out, "def456") {
		t.Errorf("the surviving file was dropped:\n%s", out)
	}
}

// A partition with no registry, or no site set, must not write anything — the
// dev-mode and cold-boot paths both land here.
func TestReconcileGfehPublicIndexesNoopsWithoutInput(t *testing.T) {
	base := t.TempDir()

	if got := reconcileGfehPublicIndexes(context.Background(), nil, base, homeSites(t)); got != nil {
		t.Errorf("a nil registry wrote %v", got)
	}
	reg, _ := publishedRegistry()
	if got := reconcileGfehPublicIndexes(context.Background(), reg, base, nil); got != nil {
		t.Errorf("an empty site set wrote %v", got)
	}
	if _, err := os.Stat(filepath.Join(base, GfehIndexDirName)); !os.IsNotExist(err) {
		t.Error("an index root was created with nothing to put in it")
	}
}
