package packages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// testGitSeedFile is the filename written into the test bare repo. Kept as
// a package-level constant so buildBareRepo and pushUpdate agree on it.
const testGitSeedFile = "index.html"

// buildBareRepo builds a bare repository with a single commit containing
// contents on the go-git default branch (master) and returns its file://
// URL so DefaultGitCloner can clone it. The working copy used to seed the
// bare repo is also returned so callers can push additional commits via
// pushUpdate.
func buildBareRepo(t *testing.T, contents string) (bareURL, workDir string) {
	t.Helper()

	bareDir := filepath.Join(t.TempDir(), "bare.git")
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}

	workDir = filepath.Join(t.TempDir(), "work")
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("PlainInit work: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}

	if err := os.WriteFile(filepath.Join(workDir, testGitSeedFile), []byte(contents), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := wt.Add(testGitSeedFile); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	return "file://" + bareDir, workDir
}

// pushUpdate commits and pushes a new revision of testGitSeedFile to the
// bare repo that workDir is cloned from.
func pushUpdate(t *testing.T, workDir, contents string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(workDir, testGitSeedFile), []byte(contents), 0644); err != nil {
		t.Fatalf("WriteFile update: %v", err)
	}
	repo, err := gogit.PlainOpen(workDir)
	if err != nil {
		t.Fatalf("PlainOpen work: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := wt.Add(testGitSeedFile); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := wt.Commit("update", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestDefaultGitClonerCloneEmptyURL(t *testing.T) {
	t.Parallel()
	err := DefaultGitCloner{}.Clone(t.TempDir(), "", "master")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultGitClonerCloneIntoExistingDir(t *testing.T) {
	t.Parallel()
	bareURL, _ := buildBareRepo(t, "seed content")

	target := filepath.Join(t.TempDir(), "vol")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Leftover file that Clone must wipe before go-git can populate the dir.
	if err := os.WriteFile(filepath.Join(target, "stale"), []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile stale: %v", err)
	}

	if err := (DefaultGitCloner{}).Clone(target, bareURL, "master"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile index.html: %v", err)
	}
	if string(got) != "seed content" {
		t.Fatalf("expected %q, got %q", "seed content", string(got))
	}
	if _, err := os.Stat(filepath.Join(target, "stale")); err == nil {
		t.Fatal("expected stale file to be removed before clone")
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatalf("expected .git directory after clone: %v", err)
	}
}

func TestDefaultGitClonerCloneDefaultBranch(t *testing.T) {
	t.Parallel()
	bareURL, _ := buildBareRepo(t, "default branch")

	target := filepath.Join(t.TempDir(), "vol")
	// Empty branch means "let the server pick HEAD" which is main.
	if err := (DefaultGitCloner{}).Clone(target, bareURL, ""); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "default branch" {
		t.Fatalf("got %q", string(got))
	}
}

func TestDefaultGitClonerUpdate(t *testing.T) {
	t.Parallel()
	bareURL, workDir := buildBareRepo(t, "v1")

	target := filepath.Join(t.TempDir(), "vol")
	if err := (DefaultGitCloner{}).Clone(target, bareURL, "master"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	pushUpdate(t, workDir, "v2")

	if err := (DefaultGitCloner{}).Update(target, "master"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("expected v2 after update, got %q", string(got))
	}
}

func TestDefaultGitClonerUpdateDiscardsLocalChanges(t *testing.T) {
	t.Parallel()
	bareURL, workDir := buildBareRepo(t, "v1")

	target := filepath.Join(t.TempDir(), "vol")
	if err := (DefaultGitCloner{}).Clone(target, bareURL, "master"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Simulate local drift in the cloned worktree.
	if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("local drift"), 0644); err != nil {
		t.Fatalf("WriteFile drift: %v", err)
	}

	pushUpdate(t, workDir, "v2")

	if err := (DefaultGitCloner{}).Update(target, "master"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "index.html"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("expected hard reset to v2, got %q", string(got))
	}
}

func TestDefaultGitClonerUpdateNotARepo(t *testing.T) {
	t.Parallel()
	err := DefaultGitCloner{}.Update(t.TempDir(), "master")
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractBasicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		in         string
		wantURL    string
		wantAuth   bool
		wantUser   string
		wantPass   string
	}{
		{
			name:    "no credentials",
			in:      "https://example.com/repo.git",
			wantURL: "https://example.com/repo.git",
		},
		{
			name:     "username and password",
			in:       "https://user:pass@example.com/repo.git",
			wantURL:  "https://example.com/repo.git",
			wantAuth: true,
			wantUser: "user",
			wantPass: "pass",
		},
		{
			name:    "empty username",
			in:      "https://:pass@example.com/repo.git",
			wantURL: "https://:pass@example.com/repo.git",
		},
		{
			name:    "malformed url",
			in:      "::not a url::",
			wantURL: "::not a url::",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotURL, gotAuth := extractBasicAuth(tc.in)
			if gotURL != tc.wantURL {
				t.Fatalf("URL: got %q, want %q", gotURL, tc.wantURL)
			}
			if tc.wantAuth {
				if gotAuth == nil {
					t.Fatal("expected auth, got nil")
				}
				if gotAuth.Username != tc.wantUser || gotAuth.Password != tc.wantPass {
					t.Fatalf("auth: got %s:%s, want %s:%s", gotAuth.Username, gotAuth.Password, tc.wantUser, tc.wantPass)
				}
			} else if gotAuth != nil {
				t.Fatalf("expected no auth, got %+v", gotAuth)
			}
		})
	}
}
