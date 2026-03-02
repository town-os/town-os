package systemcontroller

import (
	"context"

	"gitea.com/town-os/town-os/src/monitoring"
)

// MonitoringStatus returns the current monitoring stack status from the mock.
// Returns a default empty status when MonitoringStatusResp is nil.
//
// Calls GET /monitoring/status on the Control Plane Service.
func (m *MockClient) MonitoringStatus(_ context.Context) (*monitoring.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "MonitoringStatus"})

	if m.MonitoringStatusErr != nil {
		return nil, m.MonitoringStatusErr
	}

	if m.MonitoringStatusResp != nil {
		return m.MonitoringStatusResp, nil
	}

	return &monitoring.Status{}, nil
}
