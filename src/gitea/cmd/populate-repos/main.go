// populate-repos caches test package repositories as bare clones from GitHub
// and pushes them into a local Gitea instance. It is idempotent: existing
// non-empty Gitea repos are skipped, and existing bare caches are refreshed
// via fetch.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitconfig "github.com/go-git/go-git/v5/config"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	adminUser = "town-os"
	adminPass = "town-os-test"
	adminMail = "town-os@localhost"
)

// repo describes a GitHub repository to mirror into Gitea.
type repo struct {
	Owner string // GitHub owner
	Name  string // repository name
}

var repos = []repo{
	{Owner: "town-os", Name: "test-packages-core"},
	{Owner: "town-os", Name: "test-packages-extras"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	giteaURL := os.Getenv("GITEA_URL")
	if giteaURL == "" {
		return errors.New("GITEA_URL environment variable is required")
	}

	cacheDir := os.Getenv("GIT_CACHE_DIR")
	if cacheDir == "" {
		return errors.New("GIT_CACHE_DIR environment variable is required")
	}

	ghUser := os.Getenv("TOWN_OS_REPO_USERNAME")
	ghPass := os.Getenv("TOWN_OS_REPO_PASSWORD")

	client := &http.Client{Timeout: 2 * time.Minute}
	ctx := context.Background()

	// Phase 1: Cache repos as bare clones from GitHub.
	for _, r := range repos {
		if err := cacheRepo(ctx, cacheDir, r, ghUser, ghPass); err != nil {
			return fmt.Errorf("cache %s/%s: %w", r.Owner, r.Name, err)
		}
	}

	// Phase 2: Push cached repos to Gitea.
	for _, r := range repos {
		if err := pushToGitea(ctx, client, giteaURL, cacheDir, r); err != nil {
			return fmt.Errorf("push %s/%s: %w", r.Owner, r.Name, err)
		}
	}

	return nil
}

// cacheRepo ensures a bare clone of the repo exists in cacheDir. If it
// already exists, it fetches to refresh. Otherwise it creates a new bare clone.
func cacheRepo(ctx context.Context, cacheDir string, r repo, ghUser, ghPass string) error {
	repoPath := filepath.Join(cacheDir, r.Name+".git")
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", r.Owner, r.Name)

	var auth *githttp.BasicAuth
	if ghUser != "" && ghPass != "" {
		auth = &githttp.BasicAuth{Username: ghUser, Password: ghPass}
	}

	// Check if bare cache already exists.
	if _, err := os.Stat(repoPath); err == nil { //nolint:gosec // G703 -- repoPath from trusted cacheDir
		return fetchCache(ctx, repoPath, auth)
	}

	// Bare clone from GitHub.
	opts := &gogit.CloneOptions{
		URL:  cloneURL,
		Tags: gogit.AllTags,
	}
	if auth != nil {
		opts.Auth = auth
	}

	fmt.Fprintf(os.Stderr, "Cloning %s into cache %s\n", cloneURL, repoPath)
	_, err := gogit.PlainCloneContext(ctx, repoPath, true, opts)
	if err != nil {
		return fmt.Errorf("bare clone: %w", err)
	}
	return nil
}

// fetchCache opens an existing bare repo and fetches all refs.
func fetchCache(ctx context.Context, repoPath string, auth *githttp.BasicAuth) error {
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}

	opts := &gogit.FetchOptions{
		RemoteName: "origin",
		Tags:       gogit.AllTags,
		Prune:      true,
	}
	if auth != nil {
		opts.Auth = auth
	}

	fmt.Fprintf(os.Stderr, "Fetching updates for cache %s\n", repoPath)
	if err := repo.FetchContext(ctx, opts); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("fetch: %w", err)
	}

	// Fast-forward the local default branch to match origin. In a bare repo,
	// fetch only updates refs/remotes/origin/* — the local branch (which is
	// what gets pushed to Gitea) can fall behind.
	return fastForwardHead(repo)
}

// fastForwardHead updates the local HEAD branch to match its remote tracking
// ref. This is a no-op if HEAD is detached, not a branch, or the remote ref
// does not exist.
func fastForwardHead(repo *gogit.Repository) error {
	head, err := repo.Head()
	if err != nil {
		// No HEAD yet — nothing to fast-forward.
		return nil //nolint:nilerr // expected when repo has no commits
	}
	localBranch := head.Name()
	if !localBranch.IsBranch() {
		return nil
	}
	branchShort := localBranch.Short()
	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branchShort), true)
	if err != nil {
		// Remote ref doesn't exist — nothing to do.
		return nil //nolint:nilerr // expected when remote ref is missing
	}
	if head.Hash() != remoteRef.Hash() {
		ref := plumbing.NewHashReference(localBranch, remoteRef.Hash())
		if err := repo.Storer.SetReference(ref); err != nil {
			return fmt.Errorf("fast-forward %s: %w", branchShort, err)
		}
		fmt.Fprintf(os.Stderr, "Fast-forwarded %s to %s\n", branchShort, remoteRef.Hash().String()[:8])
	}
	return nil
}

// pushToGitea creates a repo in Gitea (if needed) and pushes all refs from
// the bare cache. If the Gitea repo already has content, it is skipped.
func pushToGitea(ctx context.Context, client *http.Client, giteaURL, cacheDir string, r repo) error {
	// Check if the repo already exists and has content.
	empty, exists, err := checkGiteaRepo(ctx, client, giteaURL, r.Name)
	if err != nil {
		return err
	}

	if exists && !empty {
		// Force-push to update the Gitea repo with any new commits.
		repoPath := filepath.Join(cacheDir, r.Name+".git")
		if err := pushRefs(ctx, repoPath, fmt.Sprintf("%s/%s/%s.git", giteaURL, adminUser, r.Name)); err != nil {
			return fmt.Errorf("update existing repo: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Updated existing Gitea repo %s\n", r.Name)
		return nil
	}

	// Delete empty repos (from a previously failed push).
	if exists && empty {
		fmt.Fprintf(os.Stderr, "Repository %s is empty, deleting for re-push\n", r.Name)
		if err := deleteRepo(ctx, client, giteaURL, r.Name); err != nil {
			return fmt.Errorf("delete empty repo: %w", err)
		}
	}

	// Create empty repo in Gitea.
	if err := createGiteaRepo(ctx, client, giteaURL, r.Name); err != nil {
		return fmt.Errorf("create repo: %w", err)
	}

	// Push all refs from the bare cache to Gitea.
	repoPath := filepath.Join(cacheDir, r.Name+".git")
	giteaPushURL := fmt.Sprintf("%s/%s/%s.git", giteaURL, adminUser, r.Name)

	if err := pushRefs(ctx, repoPath, giteaPushURL); err != nil {
		return fmt.Errorf("push refs: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Pushed %s to Gitea\n", r.Name)
	return nil
}

// checkGiteaRepo checks whether a repo exists in Gitea and whether it is empty.
// Returns (empty, exists, error).
func checkGiteaRepo(ctx context.Context, client *http.Client, giteaURL, name string) (bool, bool, error) {
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", giteaURL, adminUser, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, false, err
	}
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req) //nolint:gosec // G704 -- URL from trusted giteaURL parameter
	if err != nil {
		return false, false, fmt.Errorf("check repo: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode == http.StatusNotFound {
		return false, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return false, false, fmt.Errorf("unexpected status %d checking repo (read body: %w)", resp.StatusCode, err)
		}
		return false, false, fmt.Errorf("unexpected status %d checking repo: %s", resp.StatusCode, respBody)
	}

	var info struct {
		Empty bool `json:"empty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false, false, err
	}
	return info.Empty, true, nil
}

// createGiteaRepo creates an empty repository in Gitea via the API.
func createGiteaRepo(ctx context.Context, client *http.Client, giteaURL, name string) error {
	body := map[string]any{
		"name":          name,
		"auto_init":     false,
		"default_branch": "main",
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, giteaURL+"/api/v1/user/repos", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req) //nolint:gosec // G704 -- URL from trusted giteaURL parameter
	if err != nil {
		return fmt.Errorf("create repo request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusCreated {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("unexpected status %d creating repo (read body: %w)", resp.StatusCode, err)
		}
		return fmt.Errorf("unexpected status %d creating repo: %s", resp.StatusCode, respBody)
	}

	fmt.Fprintf(os.Stderr, "Created Gitea repo %s\n", name)
	return nil
}

// pushRefs opens the bare cache repo, adds a temporary remote for Gitea,
// and pushes all refs.
func pushRefs(ctx context.Context, repoPath, pushURL string) error {
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open cache for push: %w", err)
	}

	const remoteName = "gitea"

	// Remove existing gitea remote if present (idempotent).
	if err := repo.DeleteRemote(remoteName); err != nil {
		fmt.Fprintf(os.Stderr, "remove existing gitea remote: %v\n", err)
	}

	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: remoteName,
		URLs: []string{pushURL},
	})
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	defer func() { _ = repo.DeleteRemote(remoteName) }()

	auth := &githttp.BasicAuth{
		Username: adminUser,
		Password: adminPass,
	}

	err = repo.PushContext(ctx, &gogit.PushOptions{
		RemoteName: remoteName,
		RefSpecs:   []gitconfig.RefSpec{"+refs/*:refs/*"},
		Auth:       auth,
		Force:      true,
	})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}

// ensureAdminUser creates the admin user via Gitea's admin API.
// If the user already exists (409 Conflict), it is silently ignored.
func ensureAdminUser(ctx context.Context, client *http.Client, giteaURL string) error {
	body := map[string]any{
		"username":             adminUser,
		"password":             adminPass,
		"email":                adminMail,
		"must_change_password": false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, giteaURL+"/api/v1/admin/users", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req) //nolint:gosec // G704 -- URL from trusted giteaURL parameter
	if err != nil {
		return fmt.Errorf("create admin user request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		fmt.Fprintf(os.Stderr, "Created admin user %s\n", adminUser)
		return nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		// User already exists.
		fmt.Fprintf(os.Stderr, "Admin user %s already exists\n", adminUser)
		return nil
	default:
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("unexpected status %d creating admin user (read body: %w)", resp.StatusCode, err)
		}
		return fmt.Errorf("unexpected status %d creating admin user: %s", resp.StatusCode, respBody)
	}
}

// isRepoEmpty checks whether a repo exists and has no content.
func isRepoEmpty(ctx context.Context, client *http.Client, giteaURL, name string) (bool, error) {
	empty, exists, err := checkGiteaRepo(ctx, client, giteaURL, name)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	return empty, nil
}

// deleteRepo removes a repository via the Gitea API.
func deleteRepo(ctx context.Context, client *http.Client, giteaURL, name string) error {
	delURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", giteaURL, adminUser, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req) //nolint:gosec // G704 -- URL from trusted giteaURL parameter
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("unexpected status %d deleting repo (read body: %w)", resp.StatusCode, err)
		}
		return fmt.Errorf("unexpected status %d deleting repo: %s", resp.StatusCode, respBody)
	}
	return nil
}
