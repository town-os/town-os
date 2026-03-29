package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"gitea.com/town-os/town-os/src/monitoring"
)

// MonitoringStatus returns the current state of the monitoring stack.
//
// Calls GET /monitoring/status on the Control Plane Service.
func (c *SystemdClient) MonitoringStatus(ctx context.Context) (_ *monitoring.MonitoringStatus, err error) {
	resp, err := c.getClient(ctx, "monitoring/status")
	if err != nil {
		return nil, fmt.Errorf("%w: MonitoringStatus: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "monitoring/status")
	}

	var status monitoring.MonitoringStatus
	return &status, json.NewDecoder(resp.Body).Decode(&status)
}
