package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

// ExecClient implements Client by shelling out to the git binary.
type ExecClient struct {
	// Home overrides the HOME environment variable for git commands.
	// When empty, git inherits the process HOME.
	Home string
}

func (c *ExecClient) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if c.Home != "" {
		env = append(env, fmt.Sprintf("HOME=%s", c.Home)) //nolint:perfsprint // project convention: use fmt.Sprintf
	}
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w\n%s", args[0], err, out)
	}
	return out, nil
}

func (c *ExecClient) Clone(ctx context.Context, dir, url, name string) error {
	_, err := c.run(ctx, dir, "clone", url, name)
	return err
}

func (c *ExecClient) Pull(ctx context.Context, dir string) error {
	_, err := c.run(ctx, dir, "pull", "--rebase")
	return err
}

func (c *ExecClient) Diff(ctx context.Context, dir string) (bool, error) {
	_, err := c.run(ctx, dir, "diff", "--quiet", "HEAD")
	if err != nil {
		return true, nil //nolint:nilerr // non-zero exit means dirty
	}
	return false, nil
}

func (c *ExecClient) Stash(ctx context.Context, dir string) error {
	_, err := c.run(ctx, dir, "stash")
	return err
}

func (c *ExecClient) StashApply(ctx context.Context, dir string) error {
	_, err := c.run(ctx, dir, "stash", "apply")
	return err
}

func (c *ExecClient) Fetch(ctx context.Context, dir string) error {
	_, err := c.run(ctx, dir, "fetch")
	return err
}

func (c *ExecClient) Checkout(ctx context.Context, dir, ref string) error {
	_, err := c.run(ctx, dir, "checkout", ref)
	return err
}

func (c *ExecClient) Init(ctx context.Context, dir string) error {
	_, err := c.run(ctx, dir, "init")
	return err
}

func (c *ExecClient) Add(ctx context.Context, dir string, paths ...string) error {
	args := append([]string{"add"}, paths...)
	_, err := c.run(ctx, dir, args...)
	return err
}

func (c *ExecClient) Commit(ctx context.Context, dir, message string) error {
	_, err := c.run(ctx, dir, "commit", "-m", message)
	return err
}

func (c *ExecClient) RevParse(ctx context.Context, dir, ref string) (string, error) {
	out, err := c.run(ctx, dir, "rev-parse", ref)
	if err != nil {
		return "", err
	}

	s := string(out)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s, nil
}

func (c *ExecClient) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return c.run(ctx, dir, args...)
}
