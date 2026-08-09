package systemcontroller

import "context"

// --- Status ---

func (m *MockClient) Ping(_ context.Context) (*PingResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Ping", Args: nil})

	if m.PingErr != nil {
		return nil, m.PingErr
	}

	if m.PingResponse != nil {
		return m.PingResponse, nil
	}

	return &PingResponse{Status: "ok"}, nil
}

// GetMetrics returns the mock scrape body.
func (m *MockClient) GetMetrics(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetMetrics"})

	if m.GetMetricsErr != nil {
		return "", m.GetMetricsErr
	}
	return m.MetricsBody, nil
}
