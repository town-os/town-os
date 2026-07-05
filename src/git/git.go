package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// Client abstracts git operations so callers do not shell out directly.
type Client interface {
	// Clone clones a repository into the given directory.
	// The directory is the parent; the repository is cloned as a subdirectory
	// named by the last argument. The remote's default branch is checked out.
	Clone(ctx context.Context, dir, url, name string) error
	// CloneBranch is like Clone but checks out the named branch as a
	// single-branch clone. An empty branch is equivalent to Clone (the remote's
	// default branch).
	CloneBranch(ctx context.Context, dir, url, name, branch string) error

	// Pull runs git pull --rebase in the given directory.
	Pull(ctx context.Context, dir string) error

	// Diff returns whether the working tree is dirty.
	// It returns true when there are uncommitted changes.
	Diff(ctx context.Context, dir string) (dirty bool, err error)

	// Stash stashes uncommitted changes.
	Stash(ctx context.Context, dir string) error

	// StashApply re-applies the most recent stash entry.
	StashApply(ctx context.Context, dir string) error

	// Fetch fetches from the remote.
	Fetch(ctx context.Context, dir string) error

	// Checkout checks out a branch or commit.
	Checkout(ctx context.Context, dir, ref string) error

	// Init initializes a new git repository.
	Init(ctx context.Context, dir string) error

	// Add stages files matching the given pathspecs.
	Add(ctx context.Context, dir string, paths ...string) error

	// Commit creates a commit with the given message.
	Commit(ctx context.Context, dir, message string) error

	// RevParse returns the output of git rev-parse for the given ref.
	RevParse(ctx context.Context, dir, ref string) (string, error)

	// Run executes an arbitrary git command in the given directory.
	// Use this for operations not covered by dedicated methods.
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// GoGitClient implements Client using the go-git library.
type GoGitClient struct {
	// Home overrides the HOME environment variable for git commands.
	// When empty, git inherits the process HOME.
	Home string
}

// extractAuth parses credentials from a URL if present and returns
// the cleaned URL and an http.BasicAuth instance (or nil).
func extractAuth(rawURL string) (string, *http.BasicAuth) {
	// Quick check for embedded credentials: scheme://user:pass@host/...
	if !strings.Contains(rawURL, "@") {
		return rawURL, nil
	}

	// Parse manually to extract userinfo.
	// Format: scheme://user:pass@rest
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd == -1 {
		return rawURL, nil
	}

	afterScheme := rawURL[schemeEnd+3:]
	userinfo, rest, found := strings.Cut(afterScheme, "@")
	if !found {
		return rawURL, nil
	}
	parts := strings.SplitN(userinfo, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return rawURL, nil
	}

	cleanURL := rawURL[:schemeEnd+3] + rest
	return cleanURL, &http.BasicAuth{
		Username: parts[0],
		Password: parts[1],
	}
}

func (c *GoGitClient) Clone(ctx context.Context, dir, url, name string) error {
	return c.CloneBranch(ctx, dir, url, name, "")
}

// CloneBranch clones url into dir/name, checking out branch as a single-branch
// clone when branch is non-empty. An empty branch clones the remote's default
// branch (identical to Clone).
func (c *GoGitClient) CloneBranch(ctx context.Context, dir, url, name, branch string) error {
	target := filepath.Join(dir, name)
	cleanURL, auth := extractAuth(url)

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

	_, err := gogit.PlainCloneContext(ctx, target, false, opts)
	if err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

func (c *GoGitClient) Pull(ctx context.Context, dir string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git pull: open: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git pull: worktree: %w", err)
	}

	auth := c.remoteAuth(repo)

	opts := &gogit.PullOptions{
		RemoteName: "origin",
	}
	if auth != nil {
		opts.Auth = auth
	}

	if err := wt.PullContext(ctx, opts); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git pull: %w", err)
	}
	return nil
}

func (c *GoGitClient) Diff(_ context.Context, dir string) (bool, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return false, fmt.Errorf("git diff: open: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("git diff: worktree: %w", err)
	}

	status, err := wt.Status()
	if err != nil {
		return false, fmt.Errorf("git diff: status: %w", err)
	}

	return !status.IsClean(), nil
}

func (c *GoGitClient) Stash(_ context.Context, dir string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git stash: open: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git stash: worktree: %w", err)
	}

	// Stage all changes first so stash captures everything.
	if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
		return fmt.Errorf("git stash: add: %w", err)
	}

	// Create a stash commit: save the current index as a commit on a special ref.
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("git stash: head: %w", err)
	}

	idx, err := wt.Status()
	if err != nil {
		return fmt.Errorf("git stash: status: %w", err)
	}

	if idx.IsClean() {
		return nil
	}

	stashHash, err := wt.Commit("WIP on stash", &gogit.CommitOptions{
		All: true,
		Author: &object.Signature{
			Name:  "stash",
			Email: "stash@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("git stash: commit: %w", err)
	}

	// Store the stash ref.
	ref := plumbing.NewReferenceFromStrings("refs/stash", stashHash.String())
	if err := repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("git stash: set ref: %w", err)
	}

	// Reset HEAD back to the original commit.
	if err := wt.Reset(&gogit.ResetOptions{
		Commit: head.Hash(),
		Mode:   gogit.HardReset,
	}); err != nil {
		return fmt.Errorf("git stash: reset: %w", err)
	}

	return nil
}

func (c *GoGitClient) StashApply(_ context.Context, dir string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git stash apply: open: %w", err)
	}

	// Read the stash ref.
	stashRef, err := repo.Storer.Reference(plumbing.ReferenceName("refs/stash"))
	if err != nil {
		return fmt.Errorf("git stash apply: no stash found: %w", err)
	}

	stashCommit, err := repo.CommitObject(stashRef.Hash())
	if err != nil {
		return fmt.Errorf("git stash apply: commit: %w", err)
	}

	// Get the stash tree.
	stashTree, err := stashCommit.Tree()
	if err != nil {
		return fmt.Errorf("git stash apply: tree: %w", err)
	}

	// Get current HEAD tree for comparison.
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("git stash apply: head: %w", err)
	}

	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return fmt.Errorf("git stash apply: head commit: %w", err)
	}

	headTree, err := headCommit.Tree()
	if err != nil {
		return fmt.Errorf("git stash apply: head tree: %w", err)
	}

	// Diff the stash tree against head tree and apply changes.
	changes, err := headTree.Diff(stashTree)
	if err != nil {
		return fmt.Errorf("git stash apply: diff: %w", err)
	}

	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return fmt.Errorf("git stash apply: action: %w", err)
		}

		switch action {
		case merkletrie.Insert, merkletrie.Modify:
			// Get file content from stash tree.
			to := change.To
			f, err := stashTree.File(to.Name)
			if err != nil {
				return fmt.Errorf("git stash apply: file %s: %w", to.Name, err)
			}
			content, err := f.Contents()
			if err != nil {
				return fmt.Errorf("git stash apply: contents %s: %w", to.Name, err)
			}

			mode, err := f.Mode.ToOSFileMode()
			if err != nil {
				mode = 0644
			}

			targetPath := filepath.Join(dir, to.Name)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0750); err != nil {
				return fmt.Errorf("git stash apply: mkdir: %w", err)
			}
			if err := os.WriteFile(targetPath, []byte(content), mode); err != nil {
				return fmt.Errorf("git stash apply: write %s: %w", to.Name, err)
			}

		case merkletrie.Delete:
			targetPath := filepath.Join(dir, change.From.Name)
			if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("git stash apply: remove %s: %w", change.From.Name, err)
			}
		}
	}

	return nil
}

func (c *GoGitClient) Fetch(ctx context.Context, dir string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git fetch: open: %w", err)
	}

	auth := c.remoteAuth(repo)

	opts := &gogit.FetchOptions{
		RemoteName: "origin",
	}
	if auth != nil {
		opts.Auth = auth
	}

	if err := repo.FetchContext(ctx, opts); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}

func (c *GoGitClient) Checkout(_ context.Context, dir, ref string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git checkout: open: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git checkout: worktree: %w", err)
	}

	// Try as branch first.
	branchRef := plumbing.NewBranchReferenceName(ref)
	if _, err := repo.Reference(branchRef, true); err == nil {
		return wt.Checkout(&gogit.CheckoutOptions{
			Branch: branchRef,
		})
	}

	// Try as hash.
	if plumbing.IsHash(ref) {
		return wt.Checkout(&gogit.CheckoutOptions{
			Hash: plumbing.NewHash(ref),
		})
	}

	// Try as tag.
	tagRef := plumbing.NewTagReferenceName(ref)
	if _, err := repo.Reference(tagRef, true); err == nil {
		return wt.Checkout(&gogit.CheckoutOptions{
			Branch: tagRef,
		})
	}

	return fmt.Errorf("git checkout: reference %q not found", ref)
}

func (c *GoGitClient) Init(_ context.Context, dir string) error {
	// Verify the parent directory exists before initializing. go-git's
	// PlainInit creates the full path (including parents), but callers
	// expect an error when the parent does not exist, matching real git
	// behavior.
	if parent := filepath.Dir(dir); parent != dir {
		if _, err := os.Stat(parent); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}

	_, err := gogit.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	return nil
}

func (c *GoGitClient) Add(_ context.Context, dir string, paths ...string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git add: open: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git add: worktree: %w", err)
	}

	for _, p := range paths {
		if p == "." {
			if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
				return fmt.Errorf("git add: %w", err)
			}
			continue
		}
		if _, err := wt.Add(p); err != nil {
			return fmt.Errorf("git add %s: %w", p, err)
		}
	}

	return nil
}

func (c *GoGitClient) Commit(_ context.Context, dir, message string) error {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("git commit: open: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git commit: worktree: %w", err)
	}

	// Read author from git config, fall back to defaults.
	name, email := "Town OS", "town-os@localhost"
	cfg, err := repo.ConfigScoped(config.LocalScope)
	if err == nil && cfg.User.Name != "" {
		name = cfg.User.Name
		email = cfg.User.Email
	}

	_, err = wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

func (c *GoGitClient) RevParse(_ context.Context, dir, ref string) (string, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("git rev-parse: open: %w", err)
	}

	if ref == "HEAD" {
		head, err := repo.Head()
		if err != nil {
			return "", fmt.Errorf("git rev-parse: head: %w", err)
		}
		return head.Hash().String(), nil
	}

	// Try branch.
	branchRef := plumbing.NewBranchReferenceName(ref)
	if r, err := repo.Reference(branchRef, true); err == nil {
		return r.Hash().String(), nil
	}

	// Try tag.
	tagRef := plumbing.NewTagReferenceName(ref)
	if r, err := repo.Reference(tagRef, true); err == nil {
		return r.Hash().String(), nil
	}

	// Try as hash directly.
	if plumbing.IsHash(ref) {
		return ref, nil
	}

	return "", fmt.Errorf("git rev-parse: reference %q not found", ref)
}
