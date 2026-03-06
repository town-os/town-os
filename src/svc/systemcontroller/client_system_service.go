package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/systemd"
)

// ListSystemServices returns the list of system services with their current status.
func (c *SystemdClient) ListSystemServices(ctx context.Context) (_ []SystemServiceEntry, err error) {
	resp, err := c.getClient(ctx, "system-services")
	if err != nil {
		return nil, fmt.Errorf("%w: ListSystemServices: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "system-services")
	}

	var entries []SystemServiceEntry
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

// SetSystemServiceStatus applies an action (start, stop, restart) to a system service.
func (c *SystemdClient) SetSystemServiceStatus(ctx context.Context, key string, action systemd.StatusAction) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetSystemServiceStatusRequest{Key: key, Action: action})

	return c.postClient(ctx, "system-services/status", pr)
}
