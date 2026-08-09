package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// GetMetrics returns the Prometheus exposition-format scrape body.
//
// This exists so tests can scrape the endpoint the way Prometheus does — over
// real HTTP, through the real middleware chain — rather than calling the
// collector directly and proving only that a function returns a struct. Nothing
// in the UI consumes it: the browser reads /status/ping, and Prometheus scrapes
// this itself.
func (c *SystemdClient) GetMetrics(ctx context.Context) (_ string, err error) {
	resp, err := c.getClient(ctx, "metrics")
	if err != nil {
		return "", fmt.Errorf("%w: GetMetrics: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return "", readProblemDetail(resp, "GET", "metrics")
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("GetMetrics: read body: %w", err)
	}
	return string(data), nil
}
