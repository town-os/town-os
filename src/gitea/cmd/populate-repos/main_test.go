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
	err = fetchCache(context.Background(), barePath, nil)
	if err != nil {
		t.Fatalf("fetchCache: %v", err)
	}

	// The FETCH_HEAD or remote tracking refs should have the new commit.
	// Re-open and check refs.
	cached, err = gogit.PlainOpen(barePath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// Check that origin/master (or the default branch) has advanced.
	refs, err := cached.References()
	if err != nil {
		t.Fatalf("refs: %v", err)
	}

	var foundNewRef bool
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Hash() != oldHead.Hash() && !ref.Hash().IsZero() {
			foundNewRef = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("iterate refs: %v", err)
	}
	if !foundNewRef {
		t.Fatal("expected cache to have new refs after fetch")
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
			_, _ = w.Write([]byte(`{}`))
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
	_ = refs.ForEach(func(ref *plumbing.Reference) error {
		refCount++
		return nil
	})
	if refCount == 0 {
		t.Fatal("expected pushed refs in gitea repo")
	}
}

func TestPushToGiteaSkipsNonEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-repo":
			// Repo exists and is non-empty.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"test-repo","empty":false}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-repo"}

	err := pushToGitea(ctx, client, srv.URL, t.TempDir(), r)
	if err != nil {
		t.Fatalf("pushToGitea should skip non-empty: %v", err)
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
			_, _ = w.Write([]byte(`{"name":"test-repo","empty":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-repo":
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			createCalled = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
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
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
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
		_, _ = w.Write([]byte(`{"empty":false}`))
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
		_, _ = w.Write([]byte(`{"empty":true}`))
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
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
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
			_, _ = w.Write([]byte(`{}`))
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
		_, _ = w.Write([]byte(`{"message":"error"}`))
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
	err = fetchCache(context.Background(), barePath, nil)
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
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
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
			_, _ = w.Write([]byte(`{"message":"user already exists"}`))
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
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
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
			_, _ = w.Write([]byte(`{"message":"user already exists"}`))
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
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
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
			_, _ = w.Write([]byte(`{}`))
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
			_, _ = w.Write([]byte(`{"empty":true}`))
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
			_, _ = w.Write([]byte(`{"empty":false}`))
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
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
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
	t.Setenv("GIT_CACHE_DIR", "/tmp/test")
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
