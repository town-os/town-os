package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	// named by the last argument.
	Clone(ctx context.Context, dir, url, name string) error

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
	target := filepath.Join(dir, name)
	cleanURL, auth := extractAuth(url)

	opts := &gogit.CloneOptions{
		URL: cleanURL,
	}
	if auth != nil {
		opts.Auth = auth
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

func (c *GoGitClient) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("git run: no arguments provided")
	}

	// Handle well-known subcommands using the go-git library.
	switch args[0] {
	case "init":
		bare := false
		for _, a := range args[1:] {
			if a == "--bare" {
				bare = true
			}
		}
		if bare {
			_, err := gogit.PlainInit(dir, true)
			return nil, err
		}
		return nil, c.Init(ctx, dir)

	case "status":
		repo, err := gogit.PlainOpen(dir)
		if err != nil {
			return nil, fmt.Errorf("git status: %w", err)
		}
		wt, err := repo.Worktree()
		if err != nil {
			return nil, fmt.Errorf("git status: %w", err)
		}
		status, err := wt.Status()
		if err != nil {
			return nil, fmt.Errorf("git status: %w", err)
		}
		return []byte(status.String()), nil

	case "config":
		return c.runConfig(dir, args[1:])

	case "branch":
		return c.runBranch(dir, args[1:])

	case "rev-parse":
		return c.runRevParse(dir, args[1:])

	case "log":
		return c.runLog(dir, args[1:])

	default:
		return nil, fmt.Errorf("git run: unsupported subcommand %q (use dedicated methods)", args[0])
	}
}

// runConfig handles "git config" operations.
func (c *GoGitClient) runConfig(dir string, args []string) ([]byte, error) {
	if len(args) < 2 { //nolint:mnd // config requires key+value
		return nil, errors.New("git config: need key and value")
	}

	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("git config: open: %w", err)
	}

	cfg, err := repo.Config()
	if err != nil {
		return nil, fmt.Errorf("git config: read: %w", err)
	}

	key := args[0]
	value := args[1]

	parts := strings.SplitN(key, ".", 2) //nolint:mnd // section.key
	if len(parts) != 2 {                 //nolint:mnd // section.key
		return nil, fmt.Errorf("git config: invalid key %q", key)
	}

	section := parts[0]
	option := parts[1]

	// Handle known config sections directly.
	switch section {
	case "user":
		switch option {
		case "email":
			cfg.User.Email = value
		case "name":
			cfg.User.Name = value
		default:
			cfg.Raw.Section(section).SetOption(option, value)
		}
	default:
		cfg.Raw.Section(section).SetOption(option, value)
	}

	if err := repo.SetConfig(cfg); err != nil {
		return nil, fmt.Errorf("git config: write: %w", err)
	}
	return nil, nil
}

// runBranch handles "git branch <name>" to create branches.
func (c *GoGitClient) runBranch(dir string, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("git branch: name required")
	}

	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("git branch: open: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("git branch: head: %w", err)
	}

	branchName := args[0]
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branchName), head.Hash())
	if err := repo.Storer.SetReference(ref); err != nil {
		return nil, fmt.Errorf("git branch: create: %w", err)
	}

	return nil, nil
}

// runRevParse handles rev-parse sub-arguments.
func (c *GoGitClient) runRevParse(dir string, args []string) ([]byte, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("git rev-parse: open: %w", err)
	}

	for _, arg := range args {
		if arg == "--abbrev-ref" {
			continue
		}
		if arg == "HEAD" {
			head, err := repo.Head()
			if err != nil {
				return nil, fmt.Errorf("git rev-parse: %w", err)
			}
			if slices.Contains(args, "--abbrev-ref") {
				return []byte(head.Name().Short() + "\n"), nil
			}
			return []byte(head.Hash().String() + "\n"), nil
		}
	}

	return nil, fmt.Errorf("git rev-parse: unsupported args %v", args)
}

// runLog handles "git log" for basic output.
func (c *GoGitClient) runLog(dir string, args []string) ([]byte, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("git log: open: %w", err)
	}

	maxCount := 10
	oneline := false
	for _, arg := range args {
		if arg == "--oneline" {
			oneline = true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 1 {
			// Parse -N format.
			n := 0
			if _, err := fmt.Sscanf(arg, "-%d", &n); err == nil {
				maxCount = n
			}
		}
	}

	iter, err := repo.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var buf bytes.Buffer
	count := 0
	var iterErr error
	for count < maxCount {
		commit, err := iter.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				iterErr = err
			}
			break
		}
		if oneline {
			fmt.Fprintf(&buf, "%s %s\n", commit.Hash.String()[:7], strings.Split(commit.Message, "\n")[0])
		} else {
			fmt.Fprintf(&buf, "commit %s\nAuthor: %s <%s>\nDate:   %s\n\n    %s\n\n",
				commit.Hash.String(),
				commit.Author.Name, commit.Author.Email,
				commit.Author.When.Format(time.ANSIC),
				strings.TrimSpace(commit.Message))
		}
		count++
	}
	iter.Close()

	if iterErr != nil {
		return nil, fmt.Errorf("git log: %w", iterErr)
	}
	return buf.Bytes(), nil
}

// remoteAuth extracts authentication from the origin remote URL if present.
func (c *GoGitClient) remoteAuth(repo *gogit.Repository) *http.BasicAuth {
	remote, err := repo.Remote("origin")
	if err != nil {
		return nil
	}

	urls := remote.Config().URLs
	if len(urls) == 0 {
		return nil
	}

	_, auth := extractAuth(urls[0])
	return auth
}
