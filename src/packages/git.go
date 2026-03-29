package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// gitCmdTimeout is the default timeout for git CLI operations.
const gitCmdTimeout = 5 * time.Minute

// GitCloner clones and updates git repositories into target directories.
type GitCloner interface {
	Clone(targetDir, repoURL, branch string) error
	Update(targetDir, branch string) error
}

// DefaultGitCloner implements GitCloner using the system git binary.
type DefaultGitCloner struct{}

func (d DefaultGitCloner) Clone(targetDir, repoURL, branch string) error {
	if repoURL == "" {
		return errors.New("git clone: repository URL must not be empty")
	}

	// Ensure target directory exists.
	if err := os.MkdirAll(targetDir, 0755); err != nil { //nolint:gosec // G301 -- clone target needs read access
		return fmt.Errorf("git clone: create target dir: %w", err)
	}

	// Remove existing content so clone starts fresh.
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return fmt.Errorf("git clone: read target dir: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(targetDir, entry.Name())); err != nil {
			return fmt.Errorf("git clone: remove existing %s: %w", entry.Name(), err)
		}
	}

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, ".")

	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = targetDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, out)
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

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = targetDir
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := fetchCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch: %w\n%s", err, out)
	}

	ref := "origin/main"
	if branch != "" {
		ref = "origin/" + branch
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", ref)
	resetCmd.Dir = targetDir
	resetCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err = resetCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git reset: %w\n%s", err, out)
	}

	return nil
}
