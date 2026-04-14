package packages

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// gitCmdTimeout is the default timeout for git operations.
const gitCmdTimeout = 5 * time.Minute

// GitCloner clones and updates git repositories into target directories.
type GitCloner interface {
	Clone(targetDir, repoURL, branch string) error
	Update(targetDir, branch string) error
}

// DefaultGitCloner implements GitCloner using the go-git library.
// No external git binary is required at runtime.
type DefaultGitCloner struct{}

func (d DefaultGitCloner) Clone(targetDir, repoURL, branch string) error {
	if repoURL == "" {
		return errors.New("git clone: repository URL must not be empty")
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil { //nolint:gosec // G301 -- clone target needs read access
		return fmt.Errorf("git clone: create target dir: %w", err)
	}

	// Remove existing content so the clone can land in an empty directory
	// (go-git's PlainCloneContext refuses non-empty targets).
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return fmt.Errorf("git clone: read target dir: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(targetDir, entry.Name())); err != nil {
			return fmt.Errorf("git clone: remove existing %s: %w", entry.Name(), err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()

	cleanURL, auth := extractBasicAuth(repoURL)
	opts := &gogit.CloneOptions{
		URL: cleanURL,
	}
	if auth != nil {
		opts.Auth = auth
	}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}

	if _, err := gogit.PlainCloneContext(ctx, targetDir, false, opts); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	return nil
}

func (d DefaultGitCloner) Update(targetDir, branch string) error {
	gitDir := filepath.Join(targetDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("git update: not a git repository: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()

	repo, err := gogit.PlainOpen(targetDir)
	if err != nil {
		return fmt.Errorf("git update: open: %w", err)
	}

	fetchOpts := &gogit.FetchOptions{
		RemoteName: "origin",
		Force:      true,
	}
	if auth := remoteBasicAuth(repo); auth != nil {
		fetchOpts.Auth = auth
	}
	if err := repo.FetchContext(ctx, fetchOpts); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git fetch: %w", err)
	}

	refName := branch
	if refName == "" {
		refName = "main"
	}
	remoteRef := plumbing.NewRemoteReferenceName("origin", refName)
	ref, err := repo.Reference(remoteRef, true)
	if err != nil {
		return fmt.Errorf("git update: resolve origin/%s: %w", refName, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git update: worktree: %w", err)
	}
	if err := wt.Reset(&gogit.ResetOptions{Commit: ref.Hash(), Mode: gogit.HardReset}); err != nil {
		return fmt.Errorf("git update: reset: %w", err)
	}

	return nil
}

// extractBasicAuth pulls embedded credentials from a URL and returns the
// cleaned URL alongside the BasicAuth value (or nil when no credentials
// are present). Mirrors the helper in src/git but lives here to avoid a
// cyclic dependency via src/git → src/packages.
func extractBasicAuth(rawURL string) (string, *http.BasicAuth) {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL, nil
	}
	username := u.User.Username()
	password, _ := u.User.Password()
	if username == "" {
		return rawURL, nil
	}
	u.User = nil
	return u.String(), &http.BasicAuth{Username: username, Password: password}
}

// remoteBasicAuth inspects the origin remote URLs and returns any embedded
// BasicAuth credentials.
func remoteBasicAuth(repo *gogit.Repository) *http.BasicAuth {
	remote, err := repo.Remote("origin")
	if err != nil || remote == nil {
		return nil
	}
	for _, rURL := range remote.Config().URLs {
		if _, auth := extractBasicAuth(rURL); auth != nil {
			return auth
		}
	}
	return nil
}
