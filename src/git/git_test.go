package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Interface conformance ---

func TestGoGitClientImplementsClient(t *testing.T) {
	var _ Client = (*GoGitClient)(nil)
}

func TestMockClientImplementsClient(t *testing.T) {
	var _ Client = (*MockClient)(nil)
}

// initTestRepo creates an initialized git repo in a temp directory with
// user config and GPG signing disabled so commits work in any environment.
func initTestRepo(t *testing.T) (string, *GoGitClient) {
	t.Helper()
	dir := t.TempDir()
	c := &GoGitClient{}
	ctx := context.Background()

	if err := c.Init(ctx, dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, cfg := range [][2]string{
		{"user.email", "test@test.com"},
		{"user.name", "Test"},
		{"commit.gpgSign", "false"},
		{"tag.gpgSign", "false"},
	} {
		if _, err := c.Run(ctx, dir, "config", cfg[0], cfg[1]); err != nil {
			t.Fatalf("config %s: %v", cfg[0], err)
		}
	}
	return dir, c
}

// initTestRepoWithCommit creates a test repo and adds an initial commit.
func initTestRepoWithCommit(t *testing.T) (string, *GoGitClient) {
	t.Helper()
	dir, c := initTestRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0644); err != nil { //nolint:gosec // test code
		t.Fatalf("write: %v", err)
	}
	if err := c.Add(ctx, dir, "."); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, dir, "init"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir, c
}

// --- GoGitClient unit tests (real git operations on local repos) ---

func TestGoGitClientInit(t *testing.T) {
	dir := t.TempDir()
	c := &GoGitClient{}

	if err := c.Init(context.Background(), dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("expected .git directory after init: %v", err)
	}
}

func TestGoGitClientAddAndCommit(t *testing.T) {
	dir, c := initTestRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil { //nolint:gosec // test code
		t.Fatalf("write file: %v", err)
	}

	if err := c.Add(ctx, dir, "hello.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := c.Commit(ctx, dir, "initial commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	sha, err := c.RevParse(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if len(sha) < 7 {
		t.Fatalf("expected a SHA, got %q", sha)
	}
}

func TestGoGitClientDiffClean(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	dirty, err := c.Diff(context.Background(), dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if dirty {
		t.Fatal("expected clean working tree")
	}
}

func TestGoGitClientDiffDirty(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed"), 0644); err != nil { //nolint:gosec // test code
		t.Fatalf("write: %v", err)
	}

	dirty, err := c.Diff(context.Background(), dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty working tree")
	}
}

func TestGoGitClientCloneLocal(t *testing.T) {
	// Create a non-bare repo with a commit, then clone from it.
	source, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	target := t.TempDir()
	name := "cloned"
	if err := c.Clone(ctx, target, source, name); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, name, ".git")); err != nil {
		t.Fatalf("expected .git in cloned repo: %v", err)
	}
}

func TestGoGitClientStash(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	// Write original content for comparison.
	origData, err := os.ReadFile(filepath.Join(dir, "f.txt")) //nolint:gosec // test code
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Modify and stash.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("modified"), 0644); err != nil { //nolint:gosec // test code
		t.Fatalf("write: %v", err)
	}

	if err := c.Stash(ctx, dir); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "f.txt")) //nolint:gosec // test code
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != string(origData) {
		t.Fatalf("expected original after stash, got %q", data)
	}

	if err := c.StashApply(ctx, dir); err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	data, err = os.ReadFile(filepath.Join(dir, "f.txt")) //nolint:gosec // test code
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "modified" {
		t.Fatalf("expected modified after stash apply, got %q", data)
	}
}

func TestGoGitClientRevParse(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	sha, err := c.RevParse(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}

	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %q (len %d)", sha, len(sha))
	}
}

func TestGoGitClientRunStatus(t *testing.T) {
	dir := t.TempDir()
	c := &GoGitClient{}
	ctx := context.Background()

	if err := c.Init(ctx, dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	out, err := c.Run(ctx, dir, "status")
	if err != nil {
		t.Fatalf("Run status: %v", err)
	}

	// Empty repo status returns empty string from go-git, which is valid.
	_ = out
}

func TestGoGitClientHomeOverride(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	c := &GoGitClient{Home: home}
	ctx := context.Background()

	if err := c.Init(ctx, dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if _, err := c.Run(ctx, dir, "status"); err != nil {
		t.Fatalf("Run with Home override: %v", err)
	}
}

func TestGoGitClientCheckout(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	if _, err := c.Run(ctx, dir, "branch", "feature"); err != nil {
		t.Fatalf("branch: %v", err)
	}
	if err := c.Checkout(ctx, dir, "feature"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	out, err := c.Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "feature" {
		t.Fatalf("expected branch feature, got %q", branch)
	}
}

func TestGoGitClientRunLog(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	out, err := c.Run(ctx, dir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatalf("Run log: %v", err)
	}

	if len(out) == 0 {
		t.Fatal("expected non-empty output from git log")
	}
}

func TestExtractAuth(t *testing.T) {
	t.Run("no credentials", func(t *testing.T) {
		url, auth := extractAuth("https://github.com/user/repo.git")
		if auth != nil {
			t.Fatal("expected no auth")
		}
		if url != "https://github.com/user/repo.git" {
			t.Fatalf("expected unchanged URL, got %s", url)
		}
	})

	t.Run("with credentials", func(t *testing.T) {
		url, auth := extractAuth("https://user:pass@github.com/user/repo.git")
		if auth == nil {
			t.Fatal("expected auth")
		}
		if auth.Username != "user" {
			t.Fatalf("expected username user, got %s", auth.Username)
		}
		if auth.Password != "pass" {
			t.Fatalf("expected password pass, got %s", auth.Password)
		}
		if url != "https://github.com/user/repo.git" {
			t.Fatalf("expected cleaned URL, got %s", url)
		}
	})

	t.Run("no scheme", func(t *testing.T) {
		url, auth := extractAuth("github.com/user/repo.git")
		if auth != nil {
			t.Fatal("expected no auth for schemeless URL")
		}
		if url != "github.com/user/repo.git" {
			t.Fatalf("expected unchanged URL, got %s", url)
		}
	})
}
