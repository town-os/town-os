package integration_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
)

func testGitCloneURL() string {
	if u := os.Getenv(packages.EnvTestRepoCoreURL); u != "" {
		return u
	}
	return "https://github.com/town-os/test-packages-core.git"
}

func TestGitClientClonePublicRepo(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Verify the clone produced a directory with .git.
	if _, err := os.Stat(filepath.Join(dir, "core", ".git")); err != nil {
		t.Fatalf("expected .git directory: %v", err)
	}
}

func TestGitClientCloneWithCredentials(t *testing.T) {
	user := os.Getenv("TOWN_OS_REPO_USERNAME")
	pass := os.Getenv("TOWN_OS_REPO_PASSWORD")

	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	// Build credential URL by embedding credentials into the test repo URL.
	repoURL := testGitCloneURL()
	if user != "" && pass != "" {
		parsed, err := url.Parse(repoURL)
		if err != nil {
			t.Fatalf("parse repo URL: %v", err)
		}
		parsed.User = url.UserPassword(user, pass)
		repoURL = parsed.String()
	}


	err := c.Clone(ctx, dir, repoURL, "core")
	if err != nil {
		t.Fatalf("Clone with credentials: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "core", ".git")); err != nil {
		t.Fatalf("expected .git directory: %v", err)
	}
}

func TestGitClientPullAfterClone(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")
	err = c.Pull(ctx, repoDir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
}

func TestGitClientDiffCloneIsClean(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")
	dirty, err := c.Diff(ctx, repoDir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if dirty {
		t.Fatal("freshly cloned repo should be clean")
	}
}

func TestGitClientDiffAfterModification(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")

	// Modify a tracked file.
	files, err := os.ReadDir(repoDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, f := range files {
		if !f.IsDir() && !strings.HasPrefix(f.Name(), ".") {
			err := os.WriteFile(filepath.Join(repoDir, f.Name()), []byte("modified-content"), 0644) //nolint:gosec // test code
			if err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			break
		}
	}

	dirty, err := c.Diff(ctx, repoDir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty after modification")
	}
}

func TestGitClientStashAndApply(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")

	// Modify a tracked file.
	testFile := filepath.Join(repoDir, "README.md")
	origData, err := os.ReadFile(testFile) //nolint:gosec // test code
	if err != nil {
		// If README.md doesn't exist, try another file.
		t.Skipf("no README.md to modify: %v", err)
	}

	if err := os.WriteFile(testFile, []byte("modified for stash test"), 0644); err != nil { //nolint:gosec // test code
		t.Fatalf("WriteFile: %v", err)
	}

	err = c.Stash(ctx, repoDir)
	if err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// File should be restored.
	data, err := os.ReadFile(testFile) //nolint:gosec // test code
	if err != nil {
		t.Fatalf("ReadFile after stash: %v", err)
	}
	if string(data) != string(origData) {
		t.Fatal("file should be restored after stash")
	}

	// Apply stash.
	err = c.StashApply(ctx, repoDir)
	if err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	data, err = os.ReadFile(testFile) //nolint:gosec // test code
	if err != nil {
		t.Fatalf("ReadFile after stash apply: %v", err)
	}
	if string(data) != "modified for stash test" {
		t.Fatalf("expected modified content after stash apply, got %q", data)
	}
}

func TestGitClientRevParseOnClone(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")
	sha, err := c.RevParse(ctx, repoDir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %q (len %d)", sha, len(sha))
	}
}

func TestGitClientFetchOnClone(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")
	if err := c.Fetch(ctx, repoDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestGitClientCloneFailsBadURL(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, "https://github.com/town-os/does-not-exist-12345.git", "bad")
	if err == nil {
		t.Fatal("expected error for nonexistent repository")
	}
}

func TestGitClientRunLogOnClone(t *testing.T) {
	dir := t.TempDir()
	c := &git.GoGitClient{Home: dir}
	ctx := context.Background()

	err := c.Clone(ctx, dir, testGitCloneURL(), "core")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repoDir := filepath.Join(dir, "core")
	out, err := c.Run(ctx, repoDir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatalf("Run git log: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output from git log")
	}
}
