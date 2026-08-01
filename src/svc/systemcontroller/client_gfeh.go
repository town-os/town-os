// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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

// The Go client for the four /gfeh/partitions/* contract routes.
//
// gfeh's own Rust client is the primary consumer of those routes; this exists
// so Town OS's integration tests can drive them the same way every other
// endpoint is driven, and so a Go caller does not have to hand-roll the prefix
// handling. Both clients must agree, and gfeh's `make check-townos-sync` is
// what proves it.

// CreateGfehPartition provisions gfeh/<name> with a quota. Admin only.
//
// name carries no prefix. Returns the created volume as Town OS names it,
// which is with the prefix.
func (c *SystemdClient) CreateGfehPartition(ctx context.Context, name string, quota uint64) (_ storage.Filesystem, err error) {
	return c.partitionCall(ctx, "gfeh/partitions/create", PartitionRequest{Name: name, Quota: quota})
}

// ModifyGfehPartition changes a partition's quota. Admin only.
func (c *SystemdClient) ModifyGfehPartition(ctx context.Context, name string, quota uint64) (_ storage.Filesystem, err error) {
	return c.partitionCall(ctx, "gfeh/partitions/modify", PartitionRequest{Name: name, Quota: quota})
}

// RemoveGfehPartition deletes a partition's subvolume and everything in it.
// Admin only. Quota is sent as zero and ignored, matching gfeh's client.
func (c *SystemdClient) RemoveGfehPartition(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PartitionRequest{Name: name, Quota: 0})

	return c.postClient(ctx, "gfeh/partitions/remove", pr)
}

// ListGfehPartitions returns every partition, names including the gfeh/ prefix.
//
// The response is a plain JSON array rather than a paginated PageResult — that
// is the contract, and gfeh deserializes it straight into a Vec.
func (c *SystemdClient) ListGfehPartitions(ctx context.Context) (_ []storage.Filesystem, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, struct{}{})

	resp, err := c.postJSON(ctx, "gfeh/partitions", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListGfehPartitions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "gfeh/partitions")
	}

	var out []storage.Filesystem
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// partitionCall is the shared create/modify shape: a PartitionRequest in, a
// Filesystem out.
func (c *SystemdClient) partitionCall(ctx context.Context, path string, req PartitionRequest) (_ storage.Filesystem, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, req)

	resp, err := c.postJSON(ctx, path, pr)
	if err != nil {
		return storage.Filesystem{}, fmt.Errorf("%w: %s: %w", ErrHTTPRequest, path, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return storage.Filesystem{}, readProblemDetail(resp, "POST", path)
	}

	var out storage.Filesystem
	return out, json.NewDecoder(resp.Body).Decode(&out)
}
