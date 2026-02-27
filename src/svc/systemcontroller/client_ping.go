package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// --- Status ---

// Ping returns service health status and summary counts for filesystems,
// repositories, packages, accounts, and units.
func (c *SystemdClient) Ping(ctx context.Context) (_ *PingResponse, err error) {
	resp, err := c.getClient(ctx, "status/ping")
	if err != nil {
		return nil, fmt.Errorf("%w: Ping: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "status/ping")
	}

	var ping PingResponse
	return &ping, json.NewDecoder(resp.Body).Decode(&ping)
}
