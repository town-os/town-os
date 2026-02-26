package systemcontroller

import (
	"context"
	"io"
)

// --- Git rebuild ---

func (c *SystemdClient) RebuildGitSources(ctx context.Context, repo, name, version string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RebuildGitRequest{Repo: repo, Name: name, Version: version})

	return c.postClient(ctx, "packages/rebuild-git", pr)
}
