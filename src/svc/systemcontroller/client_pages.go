package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/account"
)

// CreatePage creates a new static site page. The repository is cloned
// asynchronously; status transitions from "pending" to "active" on success or
// "error" on failure.
//
// Parameters:
//   - name: unique identifier for the page (primary key).
//   - repoURL: git repository URL to clone.
//   - branch: branch to check out (defaults to "main" if empty).
//   - domain: domain or subdomain to serve the page on (defaults to name if empty).
//
// Calls POST /pages/create on the Control Plane Service.
func (c *SystemdClient) CreatePage(ctx context.Context, name, repoURL, branch, domain string) (_ *account.PageSite, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreatePageRequest{Name: name, RepoURL: repoURL, Branch: branch, Domain: domain})

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
//   - fields: partial update with optional RepoURL, Branch, and Domain fields.
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
	resp, err := c.getClient(ctx, fmt.Sprintf("pages%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
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

// RebuildPage pulls the latest changes from the page's git repository. If the
// .git directory is missing, performs a fresh clone; otherwise pulls with rebase.
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
