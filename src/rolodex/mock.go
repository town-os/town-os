package rolodex

import (
	"context"
	"slices"
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

	Calls            []MockCall
	Records          []*upstream.DnsRecord
	AuthZones        []string

	AddRecordErr            error
	RemoveRecordErr         error
	RemoveCount             uint32
	ListRecordsErr          error
	FlushCacheErr           error
	CloseErr                error
	AddAuthZoneErr          error
	RemoveAuthZoneErr       error
	ListAuthZonesErr        error
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
	return m.removeRecordLocked(name, opts), nil
}

func (m *MockClient) removeRecordLocked(name string, opts *upstream.RemoveRecordOptions) uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []*upstream.DnsRecord
	var removed uint32
	for _, r := range m.Records {
		if r.Name == name && (opts == nil || opts.RecordType == nil || *opts.RecordType == r.RecordType) {
			removed++
		} else {
			kept = append(kept, r)
		}
	}
	m.Records = kept
	if m.RemoveCount > 0 {
		return m.RemoveCount
	}
	return removed
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

func (m *MockClient) AddAuthoritativeZone(_ context.Context, zone string) error {
	m.record("AddAuthoritativeZone", zone)
	if m.AddAuthZoneErr != nil {
		return m.AddAuthZoneErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.Contains(m.AuthZones, zone) {
		return nil
	}
	m.AuthZones = append(m.AuthZones, zone)
	return nil
}

func (m *MockClient) RemoveAuthoritativeZone(_ context.Context, zone string) error {
	m.record("RemoveAuthoritativeZone", zone)
	if m.RemoveAuthZoneErr != nil {
		return m.RemoveAuthZoneErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, z := range m.AuthZones {
		if z == zone {
			m.AuthZones = append(m.AuthZones[:i], m.AuthZones[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MockClient) ListAuthoritativeZones(_ context.Context) ([]string, error) {
	m.record("ListAuthoritativeZones")
	if m.ListAuthZonesErr != nil {
		return nil, m.ListAuthZonesErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.AuthZones))
	copy(out, m.AuthZones)
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
