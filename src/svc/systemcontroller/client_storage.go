package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/packages"
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
//
// Parameters:
//   - name: the current full filesystem name (e.g. "myvolume" for user volumes,
//     or "installed/repo/pkg/1.0/data" for package volumes).
//   - fs: the desired state. fs.Name is the new name (must equal name for package
//     volumes; renaming is only allowed for user filesystems). fs.Quota is the
//     new quota in bytes (0 means unlimited).
//
// The Control Plane Service rejects rename attempts on package volumes
// (installed/ or uninstalled/ prefix) with storage.ErrPackageVolumeRename.
//
// Calls POST /storage/modify on the Control Plane Service.
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

// ListPackageVolumes returns package volumes grouped by package name.
func (c *SystemdClient) ListPackageVolumes(ctx context.Context, includeUninstalled bool) ([]PackageVolumeGroup, error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageVolumesRequest{IncludeUninstalled: includeUninstalled})

	resp, err := c.postJSON(ctx, "storage/package-volumes", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackageVolumes: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "storage/package-volumes")
	}

	var groups []PackageVolumeGroup
	return groups, json.NewDecoder(resp.Body).Decode(&groups)
}

// ListExportedVolumes returns every volume installed packages have marked
// `exported: true` — the choices behind a `shared_volume` install question.
func (c *SystemdClient) ListExportedVolumes(ctx context.Context) ([]packages.ExportedVolume, error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, struct{}{})

	resp, err := c.postJSON(ctx, "storage/exported-volumes", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListExportedVolumes: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "storage/exported-volumes")
	}

	var out ExportedVolumesResponse
	return out.Volumes, json.NewDecoder(resp.Body).Decode(&out)
}

// RemovePackageVolume deletes a package volume by its internal name.
func (c *SystemdClient) RemovePackageVolume(ctx context.Context, internalName string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemovePackageVolumeRequest{InternalName: internalName})

	return c.postClient(ctx, "storage/remove-package-volume", pr)
}

// RemovePackageVolumeGroup cascades the delete down a whole package (or a
// single version when version != ""). The server stops every systemd unit
// in the package's dependency tree before removing any btrfs subvolume,
// so a container holding a mount cannot race the delete. When
// includeUninstalled is true the matching uninstalled/ subtree is swept
// as well.
func (c *SystemdClient) RemovePackageVolumeGroup(ctx context.Context, repo, name, version string, includeUninstalled bool) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemovePackageVolumeGroupRequest{
		Repo:               repo,
		Name:               name,
		Version:            version,
		IncludeUninstalled: includeUninstalled,
	})

	return c.postClient(ctx, "storage/remove-package-volume-group", pr)
}
