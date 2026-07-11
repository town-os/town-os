package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"gitea.com/town-os/town-os/src/account"
)

// CreatePage creates a new static site page. The repository is cloned
// asynchronously for git source types; container images are extracted
// asynchronously; archive pages remain pending until an archive is uploaded
// via UploadPageArchive.
//
// Parameters:
//   - name: unique identifier for the page (primary key).
//   - repoURL: git repository URL to clone (required for source_type "git").
//   - branch: branch to check out (defaults to "main" if empty).
//   - domain: domain or subdomain to serve the page on (defaults to name if empty).
//   - sourceType: content source — "archive" (default), "container_image", or "git".
//   - image: container image reference (required for source_type "container_image").
//   - imageDirectory: directory within the container image to extract (required for source_type "container_image").
//   - network: the network to publish the page on ("" = the default/home network).
//
// Calls POST /pages/create on the Control Plane Service.
func (c *SystemdClient) CreatePage(ctx context.Context, name, repoURL, branch, domain, sourceType, image, imageDirectory, network string) (_ *account.PageSite, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreatePageRequest{
		Name:           name,
		RepoURL:        repoURL,
		Branch:         branch,
		Domain:         domain,
		SourceType:     sourceType,
		Image:          image,
		ImageDirectory: imageDirectory,
		Network:        network,
	})

	resp, err := c.postJSON(ctx, "pages/create", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: CreatePage: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "pages/create")
	}

	var page account.PageSite
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// UpdatePage performs a partial update of a page's fields. Only provided
// (non-nil) fields in the update struct are changed.
//
// Parameters:
//   - name: the page name to update.
//   - fields: partial update with optional RepoURL, Branch, Domain, SourceType,
//     Image, and ImageDirectory fields.
//
// Calls POST /pages/update on the Control Plane Service.
func (c *SystemdClient) UpdatePage(ctx context.Context, name string, fields account.PageSiteUpdate) (_ *account.PageSite, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UpdatePageRequest{Name: name, Fields: fields})

	resp, err := c.postJSON(ctx, "pages/update", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: UpdatePage: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "pages/update")
	}

	var page account.PageSite
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// RemovePage deletes a page and its cloned repository directory.
//
// Parameters:
//   - name: the page name to remove.
//
// Calls POST /pages/remove on the Control Plane Service.
func (c *SystemdClient) RemovePage(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PageNameRequest{Name: name})

	return c.postClient(ctx, "pages/remove", pr)
}

// ListPages returns a paginated list of all page sites with sorting, search,
// and pagination support via params.
//
// Parameters:
//   - params: sorting, pagination, and search options.
//
// Calls GET /pages on the Control Plane Service.
func (c *SystemdClient) ListPages(ctx context.Context, params ListParams) (_ *PageResult[account.PageSite], err error) {
	resp, err := c.getClient(ctx, "pages"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListPages: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "pages")
	}

	var page PageResult[account.PageSite]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// RebuildPage pulls the latest changes from the page's git repository (for
// git-type pages), re-extracts from the container image (for container_image
// pages), or returns an error for archive pages (which must be rebuilt via
// UploadPageArchive).
//
// Parameters:
//   - name: the page name to rebuild.
//
// Calls POST /pages/rebuild on the Control Plane Service.
func (c *SystemdClient) RebuildPage(ctx context.Context, name string) (_ *account.PageSite, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PageNameRequest{Name: name})

	resp, err := c.postJSON(ctx, "pages/rebuild", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: RebuildPage: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "pages/rebuild")
	}

	var page account.PageSite
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// UploadPageArchive uploads and extracts an archive into the pages directory
// for the named page. Only valid for pages with source_type "archive". The
// page transitions to "active" status on success or "error" on failure.
//
// Parameters:
//   - name: page name to upload content for (required).
//   - archiveReader: reader providing the archive data (required).
//   - filename: archive filename for format detection (e.g., "site.tar.gz").
//     Supported formats: .tar, .tar.gz/.tgz, .tar.bz2/.tbz2, .tar.xz/.txz.
//
// Calls POST /pages/upload on the Control Plane Service.
func (c *SystemdClient) UploadPageArchive(ctx context.Context, name string, archiveReader io.Reader, filename string) (_ *account.PageSite, err error) {
	pr, pw := io.Pipe()

	writer := multipart.NewWriter(pw)
	go func() {
		if err := writer.WriteField("name", name); err != nil {
			pw.CloseWithError(err)
			return
		}
		part, err := writer.CreateFormFile("archive", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, archiveReader); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("pages/upload"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: POST pages/upload: %w", ErrNewRequest, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req) //nolint:gosec // G704 -- URL from trusted c.URL
	if err != nil {
		return nil, fmt.Errorf("%w: POST pages/upload: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "pages/upload")
	}

	var page account.PageSite
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}
