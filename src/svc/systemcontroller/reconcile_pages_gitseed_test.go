// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/storage"
)

// These tests cover the reconcile-time git seeding of pages added in
// reconcilePages: git-backed page content must converge onto btrfs (the bug was
// that pages were never seeded at reconcile, so a page whose create-time clone
// lost the boot DNS race stayed permanently empty). The seed reuses the same
// git->storage path as package volumes (seedGitIfEmpty), so it is idempotent
// (empty-only) and self-healing (a still-empty subvolume is retried).

// gitPage builds a git-source PageSite for the given short name (domain == name).
func gitPage(name string) account.PageSite {
	return account.PageSite{
		Name:       name,
		Domain:     name,
		RepoURL:    "https://example.com/" + name + ".git",
		Branch:     "main",
		SourceType: account.PageSourceGit,
	}
}

// cloneCalls counts Clone invocations recorded by a mock git client.
func cloneCalls(mc *git.MockClient) int {
	n := 0
	for _, c := range mc.GetCalls() {
		if c.Method == "Clone" {
			n++
		}
	}
	return n
}

// newPagesSeedEnv builds a ReconcileConfig with the given git client and pages,
// under a temp btrfs base. It pre-creates each page's (empty) content subvolume
// to stand in for what real btrfs does at CreateFilesystem time — the mock
// storage controller only tracks names, it does not create real directories,
// and seedGitIfEmpty only clones into an existing empty directory. Returns the
// context, config, and base path. TLD defaults to "home" (SettingsMgr nil).
func newPagesSeedEnv(t *testing.T, gitClient git.Client, pages ...account.PageSite) (context.Context, ReconcileConfig, string) {
	t.Helper()
	base := t.TempDir()
	pm := account.InitMockPagesManager()
	for _, p := range pages {
		if _, err := pm.Create(p.Name, p.RepoURL, p.Branch, p.Domain, p.SourceType, p.Image, p.ImageDirectory); err != nil {
			t.Fatalf("create page %s: %v", p.Name, err)
		}
		dir := pageHostname(p.Domain, "home")
		if err := os.MkdirAll(filepath.Join(base, PagesVolumePrefix, dir), 0o755); err != nil {
			t.Fatalf("pre-create subvolume for %s: %v", p.Name, err)
		}
	}
	cfg := ReconcileConfig{
		Storage:       storage.InitBtrFSFromController(base, storage.InitBtrFSMockController()),
		PagesManager:  pm,
		BtrfsBasePath: base,
		Git:           gitClient,
	}
	return context.Background(), cfg, base
}

// A git-backed page with an empty subvolume is cloned into that subvolume, with
// the clone pointed at pages/<fqdn> — i.e. the reported empty-scarlett.home is
// filled on reconcile.
func TestReconcilePagesGitSeedClonesEmptyPage(t *testing.T) {
	mc := git.InitMockClient()
	ctx, cfg, base := newPagesSeedEnv(t, mc, gitPage("scarlett"))

	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("reconcilePages: %v", err)
	}

	var found bool
	for _, c := range mc.GetCalls() {
		if c.Method != "Clone" {
			continue
		}
		parent, _ := c.Args[0].(string)
		url, _ := c.Args[1].(string)
		name, _ := c.Args[2].(string)
		if parent == filepath.Join(base, PagesVolumePrefix) &&
			url == "https://example.com/scarlett.git" && name == "scarlett.home" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Clone(parent=%s, url=.../scarlett.git, name=scarlett.home); got calls %+v",
			filepath.Join(base, PagesVolumePrefix), mc.GetCalls())
	}
}

// A page whose subvolume already has content is not re-cloned and its content is
// left untouched — reconcile is safe to run repeatedly on a healthy site.
func TestReconcilePagesGitSeedSkipsPopulatedPage(t *testing.T) {
	mc := git.InitMockClient()
	ctx, cfg, base := newPagesSeedEnv(t, mc, gitPage("scarlett"))

	sentinel := filepath.Join(base, PagesVolumePrefix, "scarlett.home", "index.html")
	if err := os.WriteFile(sentinel, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("reconcilePages: %v", err)
	}

	if n := cloneCalls(mc); n != 0 {
		t.Fatalf("expected no Clone for populated page, got %d", n)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "existing" {
		t.Fatalf("sentinel content changed: %q err=%v", got, err)
	}
}

// A failed clone is non-fatal and leaves the subvolume empty, so the next
// reconcile retries it (the boot DNS-race self-heal).
func TestReconcilePagesGitSeedFailureIsRetryable(t *testing.T) {
	mc := git.InitMockClient()
	mc.CloneErr = errors.New("clone boom")
	ctx, cfg, base := newPagesSeedEnv(t, mc, gitPage("scarlett"))

	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("reconcilePages must not fail on a clone error: %v", err)
	}

	if n := cloneCalls(mc); n != 1 {
		t.Fatalf("expected exactly 1 Clone attempt, got %d", n)
	}
	entries, err := os.ReadDir(filepath.Join(base, PagesVolumePrefix, "scarlett.home"))
	if err != nil {
		t.Fatalf("read subvolume: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty subvolume after failed clone (so reconcile retries), got %d entries", len(entries))
	}
}

// Non-git pages (archive/container) are never touched by the git seed.
func TestReconcilePagesGitSeedSkipsNonGitPage(t *testing.T) {
	mc := git.InitMockClient()
	ctx, cfg, _ := newPagesSeedEnv(t, mc, account.PageSite{
		Name:       "blog",
		Domain:     "blog",
		SourceType: account.PageSourceArchive,
	})

	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("reconcilePages: %v", err)
	}
	if n := cloneCalls(mc); n != 0 {
		t.Fatalf("expected no Clone for archive page, got %d", n)
	}
}

// End-to-end regression with a real go-git clone from a local source repo: the
// page's content actually lands on btrfs. This is the direct regression for the
// empty /town-os/pages/scarlett.home bug.
func TestReconcilePagesGitSeedRealClone(t *testing.T) {
	src := initPageSourceRepo(t, "<h1>scarlett</h1>")
	ctx, cfg, base := newPagesSeedEnv(t, &git.GoGitClient{}, account.PageSite{
		Name:       "scarlett",
		Domain:     "scarlett",
		RepoURL:    src,
		Branch:     "main",
		SourceType: account.PageSourceGit,
	})

	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("reconcilePages: %v", err)
	}

	seeded := filepath.Join(base, PagesVolumePrefix, "scarlett.home")
	got, err := os.ReadFile(filepath.Join(seeded, "index.html"))
	if err != nil {
		t.Fatalf("expected index.html seeded onto btrfs: %v", err)
	}
	if string(got) != "<h1>scarlett</h1>" {
		t.Fatalf("unexpected seeded content: %q", got)
	}
	if _, err := os.Stat(filepath.Join(seeded, ".git")); err != nil {
		t.Fatalf("expected .git in seeded page: %v", err)
	}
}

// A second reconcile over an already-seeded real page does not re-clone (the dir
// is now populated) and leaves the content intact.
func TestReconcilePagesGitSeedRealCloneIdempotent(t *testing.T) {
	src := initPageSourceRepo(t, "<h1>scarlett</h1>")
	ctx, cfg, base := newPagesSeedEnv(t, &git.GoGitClient{}, account.PageSite{
		Name:       "scarlett",
		Domain:     "scarlett",
		RepoURL:    src,
		Branch:     "main",
		SourceType: account.PageSourceGit,
	})

	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("first reconcilePages: %v", err)
	}
	if err := reconcilePages(ctx, cfg); err != nil {
		t.Fatalf("second reconcilePages: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(base, PagesVolumePrefix, "scarlett.home", "index.html"))
	if err != nil || string(got) != "<h1>scarlett</h1>" {
		t.Fatalf("content not intact after repeated reconcile: %q err=%v", got, err)
	}
}

// initPageSourceRepo creates a local non-bare git repo containing index.html,
// mirroring src/git/git_test.go's helper, and returns its path for use as a
// page RepoURL. go-git clones a plain local path directly (no file:// needed).
func initPageSourceRepo(t *testing.T, indexContent string) string {
	t.Helper()
	dir := t.TempDir()
	c := &git.GoGitClient{}
	ctx := context.Background()

	if err := c.Init(ctx, dir); err != nil {
		t.Fatalf("git init: %v", err)
	}
	for _, cfg := range [][2]string{
		{"user.email", "test@test.com"},
		{"user.name", "Test"},
		{"commit.gpgSign", "false"},
		{"tag.gpgSign", "false"},
	} {
		if _, err := c.Run(ctx, dir, "config", cfg[0], cfg[1]); err != nil {
			t.Fatalf("git config %s: %v", cfg[0], err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := c.Add(ctx, dir, "."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := c.Commit(ctx, dir, "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return dir
}
