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

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0644); err != nil {
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

	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
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

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed"), 0644); err != nil {
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
	origData, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Modify and stash.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("modified"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := c.Stash(ctx, dir); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != string(origData) {
		t.Fatalf("expected original after stash, got %q", data)
	}

	if err := c.StashApply(ctx, dir); err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	data, err = os.ReadFile(filepath.Join(dir, "f.txt"))
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

	t.Run("no at sign", func(t *testing.T) {
		url, auth := extractAuth("https://github.com/user/repo.git")
		if auth != nil {
			t.Fatal("expected no auth")
		}
		if url != "https://github.com/user/repo.git" {
			t.Fatalf("expected unchanged URL, got %s", url)
		}
	})

	t.Run("empty username", func(t *testing.T) {
		url, auth := extractAuth("https://:pass@github.com/repo.git")
		if auth != nil {
			t.Fatal("expected no auth for empty username")
		}
		if url != "https://:pass@github.com/repo.git" {
			t.Fatalf("expected unchanged URL, got %s", url)
		}
	})

	t.Run("user without password", func(t *testing.T) {
		url, auth := extractAuth("https://user@github.com/repo.git")
		if auth != nil {
			t.Fatal("expected no auth when no colon separator")
		}
		if url != "https://user@github.com/repo.git" {
			t.Fatalf("expected unchanged URL, got %s", url)
		}
	})

	t.Run("empty password", func(t *testing.T) {
		url, auth := extractAuth("https://user:@github.com/repo.git")
		if auth == nil {
			t.Fatal("expected auth with empty password")
		}
		if auth.Username != "user" {
			t.Fatalf("expected username user, got %s", auth.Username)
		}
		if auth.Password != "" {
			t.Fatalf("expected empty password, got %s", auth.Password)
		}
		if url != "https://github.com/repo.git" {
			t.Fatalf("expected cleaned URL, got %s", url)
		}
	})

	t.Run("http scheme", func(t *testing.T) {
		url, auth := extractAuth("http://user:pass@example.com/repo.git")
		if auth == nil {
			t.Fatal("expected auth")
		}
		if url != "http://example.com/repo.git" {
			t.Fatalf("expected cleaned URL, got %s", url)
		}
	})
}

// --- Error path tests ---

func TestGoGitClientInitInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	// Init on a path inside a non-existent directory should fail.
	err := c.Init(context.Background(), "/nonexistent/path/repo")
	if err == nil {
		t.Fatal("expected error for init in nonexistent directory")
	}
}

func TestGoGitClientDiffInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	_, err := c.Diff(context.Background(), "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for diff in nonexistent directory")
	}
}

func TestGoGitClientAddInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.Add(context.Background(), "/nonexistent/path", "file.txt")
	if err == nil {
		t.Fatal("expected error for add in nonexistent directory")
	}
}

func TestGoGitClientCommitInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.Commit(context.Background(), "/nonexistent/path", "msg")
	if err == nil {
		t.Fatal("expected error for commit in nonexistent directory")
	}
}

func TestGoGitClientPullInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.Pull(context.Background(), "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for pull in nonexistent directory")
	}
}

func TestGoGitClientFetchInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.Fetch(context.Background(), "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for fetch in nonexistent directory")
	}
}

func TestGoGitClientCheckoutInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.Checkout(context.Background(), "/nonexistent/path", "main")
	if err == nil {
		t.Fatal("expected error for checkout in nonexistent directory")
	}
}

func TestGoGitClientStashInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.Stash(context.Background(), "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for stash in nonexistent directory")
	}
}

func TestGoGitClientStashApplyInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	err := c.StashApply(context.Background(), "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for stash apply in nonexistent directory")
	}
}

func TestGoGitClientRevParseInvalidDir(t *testing.T) {
	c := &GoGitClient{}
	_, err := c.RevParse(context.Background(), "/nonexistent/path", "HEAD")
	if err == nil {
		t.Fatal("expected error for rev-parse in nonexistent directory")
	}
}

// --- Checkout by hash and tag ---

func TestGoGitClientCheckoutByHash(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	sha, err := c.RevParse(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}

	// Create a second commit so we can checkout back to the first.
	if err := os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("second"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}
	if err := c.Add(ctx, dir, "f2.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, dir, "second commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Checkout by full SHA.
	if err := c.Checkout(ctx, dir, sha); err != nil {
		t.Fatalf("Checkout by hash: %v", err)
	}

	curSHA, err := c.RevParse(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse after checkout: %v", err)
	}
	if curSHA != sha {
		t.Fatalf("expected HEAD=%s, got %s", sha, curSHA)
	}
}

func TestGoGitClientCheckoutNonexistentRef(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	err := c.Checkout(context.Background(), dir, "nonexistent-branch")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

// --- RevParse variants ---

func TestGoGitClientRevParseBranch(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	if _, err := c.Run(ctx, dir, "branch", "test-branch"); err != nil {
		t.Fatalf("branch: %v", err)
	}

	headSHA, err := c.RevParse(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	branchSHA, err := c.RevParse(ctx, dir, "test-branch")
	if err != nil {
		t.Fatalf("RevParse branch: %v", err)
	}

	if headSHA != branchSHA {
		t.Fatalf("expected branch SHA=%s to match HEAD=%s", branchSHA, headSHA)
	}
}

func TestGoGitClientRevParseFullHash(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	headSHA, err := c.RevParse(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	// RevParse of a full hash should return the hash itself.
	result, err := c.RevParse(ctx, dir, headSHA)
	if err != nil {
		t.Fatalf("RevParse hash: %v", err)
	}
	if result != headSHA {
		t.Fatalf("expected %s, got %s", headSHA, result)
	}
}

func TestGoGitClientRevParseNonexistent(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	_, err := c.RevParse(context.Background(), dir, "no-such-ref")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

// --- Pull/Fetch on local clones ---

func TestGoGitClientPullAlreadyUpToDate(t *testing.T) {
	source, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	target := t.TempDir()
	if err := c.Clone(ctx, target, source, "cloned"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Pull when already up to date should succeed.
	clonedDir := filepath.Join(target, "cloned")
	if err := c.Pull(ctx, clonedDir); err != nil {
		t.Fatalf("Pull: %v", err)
	}
}

func TestGoGitClientFetchAlreadyUpToDate(t *testing.T) {
	source, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	target := t.TempDir()
	if err := c.Clone(ctx, target, source, "cloned"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Fetch when already up to date should succeed.
	clonedDir := filepath.Join(target, "cloned")
	if err := c.Fetch(ctx, clonedDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

// --- Stash edge cases ---

func TestGoGitClientStashCleanTree(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	// Stash on a clean tree should be a no-op.
	if err := c.Stash(context.Background(), dir); err != nil {
		t.Fatalf("Stash on clean tree: %v", err)
	}
}

func TestGoGitClientStashApplyNoStash(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	// StashApply when no stash exists should error.
	err := c.StashApply(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for stash apply with no stash")
	}
	if !strings.Contains(err.Error(), "no stash found") {
		t.Fatalf("expected 'no stash found' in error, got: %v", err)
	}
}

func TestGoGitClientStashNewFile(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	// Add a new file, stash it, verify it's gone, then apply.
	newFile := filepath.Join(dir, "newfile.txt")
	if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}

	if err := c.Stash(ctx, dir); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatal("expected new file to be removed after stash")
	}

	if err := c.StashApply(ctx, dir); err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	data, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("read after apply: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("expected 'new content', got %q", data)
	}
}

func TestGoGitClientStashDeletedFile(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	// Delete the committed file and stash that deletion.
	if err := os.Remove(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := c.Stash(ctx, dir); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// After stash, the file should be restored.
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatalf("expected f.txt to be restored after stash: %v", err)
	}

	if err := c.StashApply(ctx, dir); err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	// After apply, the file should be deleted again.
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !os.IsNotExist(err) {
		t.Fatal("expected f.txt to be deleted after stash apply")
	}
}

// --- Run subcommand tests ---

func TestGoGitClientRunNoArgs(t *testing.T) {
	c := &GoGitClient{}
	_, err := c.Run(context.Background(), "/tmp", /* no args */)
	if err == nil {
		t.Fatal("expected error for Run with no args")
	}
	if !strings.Contains(err.Error(), "no arguments") {
		t.Fatalf("expected 'no arguments' in error, got: %v", err)
	}
}

func TestGoGitClientRunUnsupportedSubcommand(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	_, err := c.Run(context.Background(), dir, "push")
	if err == nil {
		t.Fatal("expected error for unsupported subcommand")
	}
	if !strings.Contains(err.Error(), "unsupported subcommand") {
		t.Fatalf("expected 'unsupported subcommand' in error, got: %v", err)
	}
}

func TestGoGitClientRunInitBare(t *testing.T) {
	dir := t.TempDir()
	bareDir := filepath.Join(dir, "bare.git")
	c := &GoGitClient{}

	_, err := c.Run(context.Background(), bareDir, "init", "--bare")
	if err != nil {
		t.Fatalf("Run init --bare: %v", err)
	}

	// Bare repos have HEAD directly in the dir, not in .git/.
	if _, err := os.Stat(filepath.Join(bareDir, "HEAD")); err != nil {
		t.Fatalf("expected HEAD in bare repo: %v", err)
	}
}

func TestGoGitClientRunConfig(t *testing.T) {
	dir, c := initTestRepo(t)
	ctx := context.Background()

	// Set a custom config value.
	if _, err := c.Run(ctx, dir, "config", "user.name", "Custom Name"); err != nil {
		t.Fatalf("config user.name: %v", err)
	}

	// Set a custom config value for user.email.
	if _, err := c.Run(ctx, dir, "config", "user.email", "custom@example.com"); err != nil {
		t.Fatalf("config user.email: %v", err)
	}

	// Set a non-user config.
	if _, err := c.Run(ctx, dir, "config", "core.autocrlf", "false"); err != nil {
		t.Fatalf("config core.autocrlf: %v", err)
	}
}

func TestGoGitClientRunConfigInvalidKey(t *testing.T) {
	dir, c := initTestRepo(t)

	_, err := c.Run(context.Background(), dir, "config", "invalid_key_no_dot", "value")
	if err == nil {
		t.Fatal("expected error for invalid config key")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("expected 'invalid key' in error, got: %v", err)
	}
}

func TestGoGitClientRunConfigMissingArgs(t *testing.T) {
	dir, c := initTestRepo(t)

	_, err := c.Run(context.Background(), dir, "config", "user.name")
	if err == nil {
		t.Fatal("expected error for config with missing value")
	}
	if !strings.Contains(err.Error(), "need key and value") {
		t.Fatalf("expected 'need key and value' in error, got: %v", err)
	}
}

func TestGoGitClientRunBranch(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	if _, err := c.Run(ctx, dir, "branch", "new-branch"); err != nil {
		t.Fatalf("branch: %v", err)
	}

	// Verify the branch was created by checking it out.
	if err := c.Checkout(ctx, dir, "new-branch"); err != nil {
		t.Fatalf("checkout new-branch: %v", err)
	}
}

func TestGoGitClientRunBranchNoName(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	_, err := c.Run(context.Background(), dir, "branch")
	if err == nil {
		t.Fatal("expected error for branch with no name")
	}
	if !strings.Contains(err.Error(), "name required") {
		t.Fatalf("expected 'name required' in error, got: %v", err)
	}
}

func TestGoGitClientRunRevParseAbbrevRef(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	out, err := c.Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse --abbrev-ref HEAD: %v", err)
	}

	branch := strings.TrimSpace(string(out))
	// The default branch name varies, but it should be non-empty.
	if branch == "" {
		t.Fatal("expected non-empty branch name")
	}
}

func TestGoGitClientRunRevParseFullHash(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	out, err := c.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	sha := strings.TrimSpace(string(out))
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %q (len %d)", sha, len(sha))
	}
}

func TestGoGitClientRunRevParseUnsupported(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	_, err := c.Run(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err == nil {
		t.Fatal("expected error for unsupported rev-parse args")
	}
}

func TestGoGitClientRunLogVerbose(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	out, err := c.Run(ctx, dir, "log", "-1")
	if err != nil {
		t.Fatalf("Run log: %v", err)
	}

	output := string(out)
	if !strings.Contains(output, "commit ") {
		t.Fatal("expected verbose log to contain 'commit '")
	}
	if !strings.Contains(output, "Author:") {
		t.Fatal("expected verbose log to contain 'Author:'")
	}
	if !strings.Contains(output, "Date:") {
		t.Fatal("expected verbose log to contain 'Date:'")
	}
}

func TestGoGitClientRunLogMultipleCommits(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	// Create a second commit.
	if err := os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("second"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}
	if err := c.Add(ctx, dir, "f2.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, dir, "second commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Request only 1 commit with oneline.
	out, err := c.Run(ctx, dir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatalf("Run log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %q", len(lines), string(out))
	}

	// Request 2 commits.
	out, err = c.Run(ctx, dir, "log", "--oneline", "-2")
	if err != nil {
		t.Fatalf("Run log -2: %v", err)
	}
	lines = strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 { //nolint:mnd // we expect exactly 2 lines
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(out))
	}
}

func TestGoGitClientRunStatusDirty(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}

	out, err := c.Run(ctx, dir, "status")
	if err != nil {
		t.Fatalf("Run status: %v", err)
	}

	// Untracked files should appear in status output.
	if len(out) == 0 {
		t.Fatal("expected non-empty status output for dirty tree")
	}
}

// --- Add with dot ---

func TestGoGitClientAddDot(t *testing.T) {
	dir, c := initTestRepo(t)
	ctx := context.Background()

	// Create multiple files.
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Add all with ".".
	if err := c.Add(ctx, dir, "."); err != nil {
		t.Fatalf("Add .: %v", err)
	}

	if err := c.Commit(ctx, dir, "add all"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify clean tree after committing all files.
	dirty, err := c.Diff(ctx, dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if dirty {
		t.Fatal("expected clean tree after adding all and committing")
	}
}

// --- Commit uses configured author ---

func TestGoGitClientCommitUsesConfiguredAuthor(t *testing.T) {
	dir, c := initTestRepo(t)
	ctx := context.Background()

	// Set custom author.
	if _, err := c.Run(ctx, dir, "config", "user.name", "Custom Author"); err != nil {
		t.Fatalf("config: %v", err)
	}
	if _, err := c.Run(ctx, dir, "config", "user.email", "custom@example.com"); err != nil {
		t.Fatalf("config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("test"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}
	if err := c.Add(ctx, dir, "test.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, dir, "test commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	out, err := c.Run(ctx, dir, "log", "-1")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	output := string(out)
	if !strings.Contains(output, "Custom Author") {
		t.Fatalf("expected author 'Custom Author' in log, got: %s", output)
	}
	if !strings.Contains(output, "custom@example.com") {
		t.Fatalf("expected email 'custom@example.com' in log, got: %s", output)
	}
}

// --- Clone into non-existent target ---

func TestGoGitClientCloneInvalidURL(t *testing.T) {
	c := &GoGitClient{}
	target := t.TempDir()

	err := c.Clone(context.Background(), target, "/nonexistent/repo", "cloned")
	if err == nil {
		t.Fatal("expected error for clone from nonexistent source")
	}
}

// --- remoteAuth ---

func TestGoGitClientRemoteAuthNoOrigin(t *testing.T) {
	dir, c := initTestRepo(t)
	_ = dir

	// Init creates a repo without a remote, so remoteAuth should return nil.
	// We test this indirectly through Fetch which calls remoteAuth.
	err := c.Fetch(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for fetch without remote")
	}
}

// --- Add multiple specific files ---

func TestGoGitClientAddMultipleFiles(t *testing.T) {
	dir, c := initTestRepo(t)
	ctx := context.Background()

	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Add two specific files.
	if err := c.Add(ctx, dir, "a.txt", "b.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, dir, "add two"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// c.txt should still be untracked, making the tree dirty.
	dirty, err := c.Diff(ctx, dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty tree since c.txt was not staged")
	}
}

// --- Run init (non-bare) through Run ---

func TestGoGitClientRunInitNonBare(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	c := &GoGitClient{}

	if _, err := c.Run(context.Background(), dir, "init"); err != nil {
		t.Fatalf("Run init: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("expected .git directory: %v", err)
	}
}

func TestGoGitClientPullWithNewCommit(t *testing.T) {
	// Create a source repo with a commit, clone it, then add a commit
	// directly to the source and pull from the clone.
	source, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	target := t.TempDir()
	if err := c.Clone(ctx, target, source, "cloned"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	clonedDir := filepath.Join(target, "cloned")

	// Add a new commit directly to the source repo.
	if err := os.WriteFile(filepath.Join(source, "update.txt"), []byte("new"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}
	if err := c.Add(ctx, source, "."); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, source, "update"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Pull from clone and verify the update arrived.
	if err := c.Pull(ctx, clonedDir); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clonedDir, "update.txt")); err != nil {
		t.Fatalf("expected update.txt after pull: %v", err)
	}
}

func TestGoGitClientFetchWithNewCommit(t *testing.T) {
	source, c := initTestRepoWithCommit(t)
	ctx := context.Background()

	target := t.TempDir()
	if err := c.Clone(ctx, target, source, "cloned"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	clonedDir := filepath.Join(target, "cloned")

	sha1, err := c.RevParse(ctx, clonedDir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	// Add a new commit directly to the source repo.
	if err := os.WriteFile(filepath.Join(source, "extra.txt"), []byte("extra"), 0644); err != nil {		t.Fatalf("write: %v", err)
	}
	if err := c.Add(ctx, source, "."); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Commit(ctx, source, "extra"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Fetch from clone, then pull to advance HEAD.
	if err := c.Fetch(ctx, clonedDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := c.Pull(ctx, clonedDir); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	sha2, err := c.RevParse(ctx, clonedDir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse HEAD after fetch+pull: %v", err)
	}

	if sha1 == sha2 {
		t.Fatal("expected HEAD SHA to change after fetch+pull")
	}
}

func TestGoGitClientRunErrorForNonexistentRef(t *testing.T) {
	dir, c := initTestRepoWithCommit(t)

	// Checkout a nonexistent branch should fail.
	err := c.Checkout(context.Background(), dir, "nonexistent-branch-xyz")
	if err == nil {
		t.Fatal("expected error for checkout of nonexistent branch")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error to mention 'not found', got: %v", err)
	}
}
