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
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

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
			if parent := filepath.Dir(dir); parent != dir {
				if _, err := os.Stat(parent); err != nil {
					return nil, fmt.Errorf("git init: %w", err)
				}
			}
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
	if len(args) < 2 {
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

	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
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
