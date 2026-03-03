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

// --- Audit ---

// ListAuditLog returns a paginated audit log. The opts parameter controls
// filtering (by account, before_id) and page size. The token parameter
// provides the bearer token for authentication.
func (c *SystemdClient) ListAuditLog(ctx context.Context, opts account.AuditListOptions, token string) (_ *account.AuditPage, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, opts)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("audit/log"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListAuditLog: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req) //nolint:gosec // G704 -- URL from trusted c.URL
	if err != nil {
		return nil, fmt.Errorf("%w: ListAuditLog: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "audit/log")
	}

	var page account.AuditPage
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}
