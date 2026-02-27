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

func (c *SystemdClient) RemovePage(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PageNameRequest{Name: name})

	return c.postClient(ctx, "pages/remove", pr)
}

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
