package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/storage"
)

// --- Storage ---

// CreateFilesystem creates a new btrfs subvolume.
func (c *SystemdClient) CreateFilesystem(ctx context.Context, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, fs)

	return c.postClient(ctx, "storage/create", pr)
}

// ModifyFilesystem renames or resizes an existing filesystem.
func (c *SystemdClient) ModifyFilesystem(ctx context.Context, name string, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ModifyFilesystemRequest{Name: name, Filesystem: fs})

	return c.postClient(ctx, "storage/modify", pr)
}

// RemoveFilesystem deletes a filesystem by name.
func (c *SystemdClient) RemoveFilesystem(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: name})

	return c.postClient(ctx, "storage/remove", pr)
}

// ListFilesystems returns a paginated list of filesystems, optionally filtered
// by name prefix and state.
func (c *SystemdClient) ListFilesystems(ctx context.Context, prefix string, state string, params ListParams) (_ *PageResult[storage.Filesystem], err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{
		Name:      prefix,
		State:     state,
		SortBy:    params.SortBy,
		SortOrder: params.SortOrder,
		Limit:     params.Limit,
		Offset:    params.Offset,
		Search:    params.Search,
	})

	resp, err := c.postJSON(ctx, "storage", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListFilesystems: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "storage")
	}

	var page PageResult[storage.Filesystem]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}
