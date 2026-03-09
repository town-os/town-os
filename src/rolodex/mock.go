package rolodex

import (
	"context"
	"sync"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// MockCall records a single method invocation on MockClient.
type MockCall struct {
	Method string
	Args   []any
}

// MockClient is an in-memory implementation of Client for use in tests.
// It records every method call in the Calls slice and returns data from its
// exported fields.
type MockClient struct {
	mu sync.Mutex

	Calls   []MockCall
	Records []*upstream.DnsRecord

	AddRecordErr    error
	RemoveRecordErr error
	RemoveCount     uint32
	ListRecordsErr  error
	FlushCacheErr   error
	CloseErr        error
}

// GetCalls returns a snapshot of all recorded method calls.
func (m *MockClient) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockClient) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

func (m *MockClient) AddRecord(_ context.Context, rec *upstream.DnsRecord) error {
	m.record("AddRecord", rec)
	if m.AddRecordErr != nil {
		return m.AddRecordErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Records = append(m.Records, rec)
	return nil
}

func (m *MockClient) RemoveRecord(_ context.Context, name string, opts *upstream.RemoveRecordOptions) (uint32, error) {
	m.record("RemoveRecord", name, opts)
	if m.RemoveRecordErr != nil {
		return 0, m.RemoveRecordErr
	}
	return m.RemoveCount, nil
}

func (m *MockClient) ListRecords(_ context.Context, opts *upstream.ListRecordsOptions) ([]*upstream.DnsRecord, error) {
	m.record("ListRecords", opts)
	if m.ListRecordsErr != nil {
		return nil, m.ListRecordsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*upstream.DnsRecord, len(m.Records))
	copy(out, m.Records)
	return out, nil
}

func (m *MockClient) FlushDnsCache(_ context.Context) error {
	m.record("FlushDnsCache")
	return m.FlushCacheErr
}

func (m *MockClient) Close() error {
	m.record("Close")
	return m.CloseErr
}
