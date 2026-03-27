package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// --- Repository ---

// AddRepository registers a new package repository with optional credentials.
func (c *SystemdClient) AddRepository(ctx context.Context, name, rawURL, username, password string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddRepositoryRequest{Name: name, URL: rawURL, Username: username, Password: password})

	return c.postClient(ctx, "repository/add", pr)
}

// RemoveRepository removes a package repository by name.
func (c *SystemdClient) RemoveRepository(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RepositoryNameRequest{Name: name})

	return c.postClient(ctx, "repository/remove", pr)
}

// MoveRepository changes the priority position of a repository.
func (c *SystemdClient) MoveRepository(ctx context.Context, name string, position int) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, MoveRepositoryRequest{Name: name, Position: position})

	return c.postClient(ctx, "repository/move", pr)
}

// RefreshRepositories triggers a refresh of all repository metadata.
// Returns a map of repository names to error messages for any that failed.
func (c *SystemdClient) RefreshRepositories(ctx context.Context) (map[string]string, error) {
	if err := c.postSSE(ctx, "repository/refresh", nil); err != nil {
		return map[string]string{"refresh": err.Error()}, err
	}
	return map[string]string{}, nil
}

// ListRepositories returns a paginated list of configured repositories.
func (c *SystemdClient) ListRepositories(ctx context.Context, params ListParams) (_ *PageResult[RepositoryInfo], err error) {
	resp, err := c.getClient(ctx, "repository"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListRepositories: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "repository")
	}

	var page PageResult[RepositoryInfo]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}
