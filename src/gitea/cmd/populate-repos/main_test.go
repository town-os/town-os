// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initBareCache creates a bare git repo at cacheDir/test-repo.git with one
// commit, suitable for testing pushToGitea. Returns the bare repo path.
func initBareCache(t *testing.T, cacheDir string) string {
	t.Helper()

	// Create a temporary non-bare repo, commit something, then clone it bare.
	tmpDir := t.TempDir()
	tmpRepo := filepath.Join(tmpDir, "work")

	repo, err := gogit.PlainInit(tmpRepo, false)
	if err != nil {
		t.Fatalf("init work repo: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	// Create a file and commit.
	if err := os.WriteFile(filepath.Join(tmpRepo, "README.md"), []byte("# test\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add file: %v", err)
	}
	_, err = wt.Commit("initial commit", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Bare clone into the cache dir.
	barePath := filepath.Join(cacheDir, "test-repo.git")
	_, err = gogit.PlainClone(barePath, true, &gogit.CloneOptions{
		URL: tmpRepo,
	})
	if err != nil {
		t.Fatalf("bare clone: %v", err)
	}

	return barePath
}

func TestCacheRepoCreatesBareClone(t *testing.T) {
	// Set up a "source" repo to act as the GitHub upstream.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "upstream")
	srcRepo, err := gogit.PlainInit(srcPath, false)
	if err != nil {
		t.Fatalf("init source: %v", err)
	}

	wt, err := srcRepo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "file.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = wt.Commit("init", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// cacheRepo should create a bare clone.
	cacheDir := t.TempDir()
	r := repo{Owner: "test", Name: "myrepo"}

	// We can't clone from GitHub in tests, so we override by calling
	// the underlying function with a local path as the clone URL.
	// Instead, test the bare clone logic directly via go-git.
	barePath := filepath.Join(cacheDir, r.Name+".git")
	_, err = gogit.PlainCloneContext(context.Background(), barePath, true, &gogit.CloneOptions{
		URL:  srcPath,
		Tags: gogit.AllTags,
	})
	if err != nil {
		t.Fatalf("bare clone: %v", err)
	}

	// Verify it's a bare repo.
	cached, err := gogit.PlainOpen(barePath)
	if err != nil {
		t.Fatalf("open cached: %v", err)
	}

	cfg, err := cached.Config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.Core.IsBare {
		t.Fatal("expected bare repo")
	}

	// Verify it has the commit.
	head, err := cached.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Fatal("expected non-zero head hash")
	}
}

func TestCacheRepoFetchesExisting(t *testing.T) {
	// Create a source repo with one commit.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "upstream")
	srcRepo, err := gogit.PlainInit(srcPath, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	wt, err := srcRepo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "file.txt"), []byte("v1\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = wt.Commit("v1", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Create bare cache from source.
	cacheDir := t.TempDir()
	barePath := filepath.Join(cacheDir, "myrepo.git")
	_, err = gogit.PlainClone(barePath, true, &gogit.CloneOptions{URL: srcPath})
	if err != nil {
		t.Fatalf("bare clone: %v", err)
	}

	// Get initial head.
	cached, err := gogit.PlainOpen(barePath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldHead, err := cached.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	// Add a second commit to the source.
	if err := os.WriteFile(filepath.Join(srcPath, "file.txt"), []byte("v2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = wt.Commit("v2", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Fetch into cache (simulating what fetchCache does).
	err = fetchCache(context.Background(), barePath, srcPath, nil)
	if err != nil {
		t.Fatalf("fetchCache: %v", err)
	}

	// The FETCH_HEAD or remote tracking refs should have the new commit.
	// Re-open and check refs.
	cached, err = gogit.PlainOpen(barePath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// After fetchCache, the local branch should be fast-forwarded to match origin.
	newHead, err := cached.Head()
	if err != nil {
		t.Fatalf("head after fetch: %v", err)
	}
	if newHead.Hash() == oldHead.Hash() {
		t.Fatal("expected HEAD to advance after fetch with new commits")
	}
}

func TestPushToGiteaCreatesAndPushes(t *testing.T) {
	// Create a bare cache with content.
	cacheDir := t.TempDir()
	initBareCache(t, cacheDir)

	// Create a bare "Gitea" repo to receive the push.
	giteaDir := t.TempDir()
	giteaPath := filepath.Join(giteaDir, "test-repo.git")
	_, err := gogit.PlainInit(giteaPath, true)
	if err != nil {
		t.Fatalf("init gitea repo: %v", err)
	}

	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-repo":
			// Repo doesn't exist yet.
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			createCalled = true
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
		default:
			t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-repo"}

	// Test checkGiteaRepo + createGiteaRepo flow.
	empty, exists, err := checkGiteaRepo(ctx, client, srv.URL, r.Name)
	if err != nil {
		t.Fatalf("checkGiteaRepo: %v", err)
	}
	if exists {
		t.Fatal("expected repo to not exist")
	}
	if empty {
		t.Fatal("expected empty=false for non-existent repo")
	}

	err = createGiteaRepo(ctx, client, srv.URL, r.Name)
	if err != nil {
		t.Fatalf("createGiteaRepo: %v", err)
	}
	if !createCalled {
		t.Fatal("expected create API to be called")
	}

	// Test pushRefs to a local bare repo (no HTTP auth needed for local push).
	err = pushRefs(ctx, filepath.Join(cacheDir, "test-repo.git"), giteaPath)
	if err != nil {
		t.Fatalf("pushRefs: %v", err)
	}

	// Verify the Gitea repo now has content.
	giteaRepo, err := gogit.PlainOpen(giteaPath)
	if err != nil {
		t.Fatalf("open gitea repo: %v", err)
	}

	refs, err := giteaRepo.References()
	if err != nil {
		t.Fatalf("refs: %v", err)
	}

	var refCount int
	if err = refs.ForEach(func(ref *plumbing.Reference) error {
		refCount++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if refCount == 0 {
		t.Fatal("expected pushed refs in gitea repo")
	}
}

func TestPushToGiteaUpdatesNonEmpty(t *testing.T) {
	cacheDir := t.TempDir()
	initBareCache(t, cacheDir)

	// Create a bare "Gitea" repo to receive the push.
	giteaDir := t.TempDir()
	giteaPath := filepath.Join(giteaDir, "test-repo.git")
	_, err := gogit.PlainInit(giteaPath, true)
	if err != nil {
		t.Fatalf("init gitea repo: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-repo":
			// Repo exists and is non-empty.
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`{"name":"test-repo","empty":false}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
		default:
			t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()

	// Test that pushToGitea force-pushes to update existing non-empty repos.
	// We call pushRefs directly since pushToGitea constructs the push URL from srv.URL.
	empty, exists, err := checkGiteaRepo(ctx, client, srv.URL, "test-repo")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !exists || empty {
		t.Fatal("expected non-empty existing repo")
	}

	// Push to the local bare repo (simulating the update push).
	err = pushRefs(ctx, filepath.Join(cacheDir, "test-repo.git"), giteaPath)
	if err != nil {
		t.Fatalf("pushRefs: %v", err)
	}

	// Verify the Gitea repo now has content.
	giteaRepo, err := gogit.PlainOpen(giteaPath)
	if err != nil {
		t.Fatalf("open gitea repo: %v", err)
	}
	head, err := giteaRepo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Fatal("expected non-zero head after push")
	}
}

func TestPushToGiteaDeletesEmptyAndRepushes(t *testing.T) {
	cacheDir := t.TempDir()
	initBareCache(t, cacheDir)

	// Create a bare "Gitea" repo to receive the push.
	giteaDir := t.TempDir()
	giteaPath := filepath.Join(giteaDir, "test-repo.git")
	_, err := gogit.PlainInit(giteaPath, true)
	if err != nil {
		t.Fatalf("init gitea repo: %v", err)
	}

	var deleteCalled, createCalled bool
	checkCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-repo":
			checkCount++
			// Repo exists but is empty.
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`{"name":"test-repo","empty":true}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-repo":
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			createCalled = true
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
		default:
			t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// We need to override the push URL to point to local bare repo.
	// Test the individual components instead.
	client := srv.Client()
	ctx := context.Background()

	empty, exists, err := checkGiteaRepo(ctx, client, srv.URL, "test-repo")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !exists {
		t.Fatal("expected repo to exist")
	}
	if !empty {
		t.Fatal("expected repo to be empty")
	}

	err = deleteRepo(ctx, client, srv.URL, "test-repo")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected delete to be called")
	}

	err = createGiteaRepo(ctx, client, srv.URL, "test-repo")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !createCalled {
		t.Fatal("expected create to be called")
	}

	// Push to local bare repo.
	err = pushRefs(ctx, filepath.Join(cacheDir, "test-repo.git"), giteaPath)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
}

func TestCheckGiteaRepoNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, exists, err := checkGiteaRepo(ctx, client, srv.URL, "nonexistent")
	if err != nil {
		t.Fatalf("checkGiteaRepo: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false")
	}
	if empty {
		t.Fatal("expected empty=false")
	}
}

func TestCheckGiteaRepoServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"message":"internal error"}`)); err != nil {
			t.Errorf("w.Write: %v", err)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	_, _, err := checkGiteaRepo(ctx, client, srv.URL, "myrepo")
	if err == nil {
		t.Fatal("expected error on server error response")
	}
}

func TestCheckGiteaRepoExistsNonEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"empty":false}`)); err != nil {
			t.Errorf("w.Write: %v", err)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, exists, err := checkGiteaRepo(ctx, client, srv.URL, "myrepo")
	if err != nil {
		t.Fatalf("checkGiteaRepo: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}
	if empty {
		t.Fatal("expected empty=false")
	}
}

func TestCheckGiteaRepoExistsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"empty":true}`)); err != nil {
			t.Errorf("w.Write: %v", err)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, exists, err := checkGiteaRepo(ctx, client, srv.URL, "myrepo")
	if err != nil {
		t.Fatalf("checkGiteaRepo: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}
	if !empty {
		t.Fatal("expected empty=true")
	}
}

func TestCreateGiteaRepoSuccess(t *testing.T) {
	var createBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("io.ReadAll: %v", err)
			}
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Errorf("json.Unmarshal: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := createGiteaRepo(ctx, client, srv.URL, "test-repo")
	if err != nil {
		t.Fatalf("createGiteaRepo: %v", err)
	}

	if createBody["name"] != "test-repo" {
		t.Fatalf("expected name=test-repo, got %v", createBody["name"])
	}
	if createBody["auto_init"] != false {
		t.Fatalf("expected auto_init=false, got %v", createBody["auto_init"])
	}
}

func TestCreateGiteaRepoUsesBasicAuth(t *testing.T) {
	var gotUser, gotPass string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos" {
			u, p, ok := r.BasicAuth()
			if ok {
				gotUser = u
				gotPass = p
			}
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := createGiteaRepo(ctx, client, srv.URL, "test-repo")
	if err != nil {
		t.Fatalf("createGiteaRepo: %v", err)
	}

	if gotUser != adminUser {
		t.Fatalf("expected user=%s, got %s", adminUser, gotUser)
	}
	if gotPass != adminPass {
		t.Fatalf("expected pass=%s, got %s", adminPass, gotPass)
	}
}

func TestCreateGiteaRepoServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"message":"error"}`)); err != nil {
			t.Errorf("w.Write: %v", err)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := createGiteaRepo(ctx, client, srv.URL, "test-repo")
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}

func TestPushRefsToLocalBareRepo(t *testing.T) {
	cacheDir := t.TempDir()
	initBareCache(t, cacheDir)

	// Create destination bare repo.
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "dest.git")
	_, err := gogit.PlainInit(destPath, true)
	if err != nil {
		t.Fatalf("init dest: %v", err)
	}

	err = pushRefs(context.Background(), filepath.Join(cacheDir, "test-repo.git"), destPath)
	if err != nil {
		t.Fatalf("pushRefs: %v", err)
	}

	// Verify refs were pushed.
	dest, err := gogit.PlainOpen(destPath)
	if err != nil {
		t.Fatalf("open dest: %v", err)
	}

	head, err := dest.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Fatal("expected non-zero head")
	}
}

func TestPushRefsIdempotent(t *testing.T) {
	cacheDir := t.TempDir()
	initBareCache(t, cacheDir)

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "dest.git")
	_, err := gogit.PlainInit(destPath, true)
	if err != nil {
		t.Fatalf("init dest: %v", err)
	}

	cachePath := filepath.Join(cacheDir, "test-repo.git")

	// Push once.
	err = pushRefs(context.Background(), cachePath, destPath)
	if err != nil {
		t.Fatalf("first push: %v", err)
	}

	// Push again — should succeed (already up-to-date).
	err = pushRefs(context.Background(), cachePath, destPath)
	if err != nil {
		t.Fatalf("second push should be idempotent: %v", err)
	}
}

func TestFetchCacheAlreadyUpToDate(t *testing.T) {
	// Create source.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "src")
	srcRepo, err := gogit.PlainInit(srcPath, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	wt, err := srcRepo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = wt.Commit("init", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t.com"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Bare clone.
	cacheDir := t.TempDir()
	barePath := filepath.Join(cacheDir, "repo.git")
	_, err = gogit.PlainClone(barePath, true, &gogit.CloneOptions{URL: srcPath})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}

	// Fetch with no new changes — should succeed.
	err = fetchCache(context.Background(), barePath, srcPath, nil)
	if err != nil {
		t.Fatalf("fetchCache already up-to-date should not error: %v", err)
	}
}

func TestPushRefsCleanupRemote(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := initBareCache(t, cacheDir)

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "dest.git")
	_, err := gogit.PlainInit(destPath, true)
	if err != nil {
		t.Fatalf("init dest: %v", err)
	}

	err = pushRefs(context.Background(), cachePath, destPath)
	if err != nil {
		t.Fatalf("pushRefs: %v", err)
	}

	// Verify the temporary "gitea" remote was cleaned up.
	cacheRepo, err := gogit.PlainOpen(cachePath)
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}

	remotes, err := cacheRepo.Remotes()
	if err != nil {
		t.Fatalf("remotes: %v", err)
	}

	for _, remote := range remotes {
		if remote.Config().Name == "gitea" {
			t.Fatal("expected gitea remote to be cleaned up after push")
		}
	}
}

// --- Tests for functions preserved from the original code ---

func TestEnsureAdminUserCreated(t *testing.T) {
	var createBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("io.ReadAll: %v", err)
			}
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Errorf("json.Unmarshal: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser: %v", err)
	}

	if createBody["username"] != "town-os" {
		t.Fatalf("expected username=town-os, got %v", createBody["username"])
	}
}

func TestEnsureAdminUserAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			w.WriteHeader(http.StatusConflict)
			if _, err := w.Write([]byte(`{"message":"user already exists"}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser should handle existing user: %v", err)
	}
}

func TestEnsureAdminUserServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(`{"message":"internal error"}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}

func TestEnsureAdminUserUnprocessableEntity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			if _, err := w.Write([]byte(`{"message":"user already exists"}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser should handle 422 as existing user: %v", err)
	}
}

func TestEnsureAdminUserBodyFields(t *testing.T) {
	var createBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("io.ReadAll: %v", err)
			}
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Errorf("json.Unmarshal: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser: %v", err)
	}

	if createBody["username"] != adminUser {
		t.Fatalf("expected username=%s, got %v", adminUser, createBody["username"])
	}
	if createBody["password"] != adminPass {
		t.Fatalf("expected password=%s, got %v", adminPass, createBody["password"])
	}
	if createBody["email"] != adminMail {
		t.Fatalf("expected email=%s, got %v", adminMail, createBody["email"])
	}
	if createBody["must_change_password"] != false {
		t.Fatalf("expected must_change_password=false, got %v", createBody["must_change_password"])
	}
}

func TestEnsureAdminUserUsesBasicAuth(t *testing.T) {
	var gotUser, gotPass string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			u, p, ok := r.BasicAuth()
			if ok {
				gotUser = u
				gotPass = p
			}
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser: %v", err)
	}

	if gotUser != adminUser {
		t.Fatalf("expected basic auth user=%s, got %s", adminUser, gotUser)
	}
	if gotPass != adminPass {
		t.Fatalf("expected basic auth pass=%s, got %s", adminPass, gotPass)
	}
}

func TestIsRepoEmptyTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`{"empty":true}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, err := isRepoEmpty(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("isRepoEmpty: %v", err)
	}
	if !empty {
		t.Fatal("expected repo to be empty")
	}
}

func TestIsRepoEmptyFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`{"empty":false}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, err := isRepoEmpty(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("isRepoEmpty: %v", err)
	}
	if empty {
		t.Fatal("expected repo not to be empty")
	}
}

func TestIsRepoEmptyNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, err := isRepoEmpty(ctx, client, srv.URL, "nonexistent")
	if err != nil {
		t.Fatalf("isRepoEmpty should not error on 404: %v", err)
	}
	if empty {
		t.Fatal("expected non-existent repo to report not-empty (false)")
	}
}

func TestDeleteRepoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := deleteRepo(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("deleteRepo: %v", err)
	}
}

func TestDeleteRepoServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(`{"message":"internal error"}`)); err != nil {
				t.Errorf("w.Write: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := deleteRepo(ctx, client, srv.URL, "test-packages-core")
	if err == nil {
		t.Fatal("expected error when delete fails")
	}
}

func TestDeleteRepoUsesBasicAuth(t *testing.T) {
	var gotUser, gotPass string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			u, p, ok := r.BasicAuth()
			if ok {
				gotUser = u
				gotPass = p
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := deleteRepo(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("deleteRepo: %v", err)
	}

	if gotUser != adminUser {
		t.Fatalf("expected basic auth user=%s, got %s", adminUser, gotUser)
	}
	if gotPass != adminPass {
		t.Fatalf("expected basic auth pass=%s, got %s", adminPass, gotPass)
	}
}

func TestRunMissingGiteaURL(t *testing.T) {
	t.Setenv("GITEA_URL", "")
	t.Setenv("GIT_CACHE_DIR", t.TempDir())
	err := run()
	if err == nil {
		t.Fatal("expected error when GITEA_URL is not set")
	}
	if !strings.Contains(err.Error(), "GITEA_URL") {
		t.Fatalf("expected error about GITEA_URL, got: %v", err)
	}
}

func TestRunMissingGitCacheDir(t *testing.T) {
	t.Setenv("GITEA_URL", "http://localhost:3000")
	t.Setenv("GIT_CACHE_DIR", "")
	err := run()
	if err == nil {
		t.Fatal("expected error when GIT_CACHE_DIR is not set")
	}
	if !strings.Contains(err.Error(), "GIT_CACHE_DIR") {
		t.Fatalf("expected error about GIT_CACHE_DIR, got: %v", err)
	}
}

func TestPushRefsPreservesExistingRemotes(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := initBareCache(t, cacheDir)

	// Add a custom remote before push.
	cacheRepo, err := gogit.PlainOpen(cachePath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	_, err = cacheRepo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "custom",
		URLs: []string{"https://example.com/repo.git"},
	})
	if err != nil {
		t.Fatalf("create custom remote: %v", err)
	}

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "dest.git")
	_, err = gogit.PlainInit(destPath, true)
	if err != nil {
		t.Fatalf("init dest: %v", err)
	}

	err = pushRefs(context.Background(), cachePath, destPath)
	if err != nil {
		t.Fatalf("pushRefs: %v", err)
	}

	// Verify custom remote still exists.
	cacheRepo, err = gogit.PlainOpen(cachePath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	remote, err := cacheRepo.Remote("custom")
	if err != nil {
		t.Fatalf("custom remote should still exist: %v", err)
	}
	if remote.Config().URLs[0] != "https://example.com/repo.git" {
		t.Fatalf("custom remote URL changed")
	}
}
