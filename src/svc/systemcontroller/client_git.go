package systemcontroller

import (
	"context"
	"io"
)

// --- Git rebuild ---

// RebuildGitSources pulls the latest changes for each git-seeded volume of an
// installed package and restarts the dependent service. Template variables are
// re-evaluated against saved responses before rebuilding.
//
// Parameters:
//   - repo: the repository name that owns the package.
//   - name: the package name.
//   - version: the installed package version.
//
// Calls POST /packages/rebuild-git on the Control Plane Service.
func (c *SystemdClient) RebuildGitSources(ctx context.Context, repo, name, version string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RebuildGitRequest{Repo: repo, Name: name, Version: version})

	return c.postClient(ctx, "packages/rebuild-git", pr)
}
