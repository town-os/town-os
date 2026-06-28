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

	Calls     []MockCall
	Records   []*upstream.DnsRecord
	AuthZones []string

	// RBL / DNSBL state.
	RblEnabled    bool
	RblProviders  []*upstream.RblConfig
	DnsblEnabled  bool
	DnsblProviders []*upstream.DnsblConfig
	LocalRblEntries []*upstream.LocalRblEntry

	AddRecordErr      error
	RemoveRecordErr   error
	RemoveCount       uint32
	ListRecordsErr    error
	FlushCacheErr     error
	CloseErr          error
	AddAuthZoneErr    error
	RemoveAuthZoneErr error
	ListAuthZonesErr  error

	SetRblConfigErr        error
	GetRblConfigErr        error
	SetDnsblConfigErr      error
	GetDnsblConfigErr      error
	AddLocalRblEntryErr    error
	RemoveLocalRblEntryErr error
	ListLocalRblEntriesErr error
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

func (m *MockClient) SetRblConfig(_ context.Context, enabled bool, providers []*upstream.RblConfig) error {
	m.record("SetRblConfig", enabled, providers)
	if m.SetRblConfigErr != nil {
		return m.SetRblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RblEnabled = enabled
	m.RblProviders = slices.Clone(providers)
	return nil
}

func (m *MockClient) GetRblConfig(_ context.Context) (*upstream.RblStatus, error) {
	m.record("GetRblConfig")
	if m.GetRblConfigErr != nil {
		return nil, m.GetRblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return &upstream.RblStatus{Enabled: m.RblEnabled, Providers: slices.Clone(m.RblProviders)}, nil
}

func (m *MockClient) SetDnsblConfig(_ context.Context, enabled bool, providers []*upstream.DnsblConfig) error {
	m.record("SetDnsblConfig", enabled, providers)
	if m.SetDnsblConfigErr != nil {
		return m.SetDnsblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DnsblEnabled = enabled
	m.DnsblProviders = slices.Clone(providers)
	return nil
}

func (m *MockClient) GetDnsblConfig(_ context.Context) (*upstream.DnsblStatus, error) {
	m.record("GetDnsblConfig")
	if m.GetDnsblConfigErr != nil {
		return nil, m.GetDnsblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return &upstream.DnsblStatus{Enabled: m.DnsblEnabled, Providers: slices.Clone(m.DnsblProviders)}, nil
}

func (m *MockClient) AddLocalRblEntry(_ context.Context, entry *upstream.LocalRblEntry) error {
	m.record("AddLocalRblEntry", entry)
	if m.AddLocalRblEntryErr != nil {
		return m.AddLocalRblEntryErr
	}
	if entry == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.LocalRblEntries {
		if e.Name == entry.Name {
			e.Reason = entry.Reason
			return nil
		}
	}
	m.LocalRblEntries = append(m.LocalRblEntries, entry)
	return nil
}

func (m *MockClient) RemoveLocalRblEntry(_ context.Context, name string) error {
	m.record("RemoveLocalRblEntry", name)
	if m.RemoveLocalRblEntryErr != nil {
		return m.RemoveLocalRblEntryErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.LocalRblEntries {
		if e.Name == name {
			m.LocalRblEntries = append(m.LocalRblEntries[:i], m.LocalRblEntries[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MockClient) ListLocalRblEntries(_ context.Context) ([]*upstream.LocalRblEntry, error) {
	m.record("ListLocalRblEntries")
	if m.ListLocalRblEntriesErr != nil {
		return nil, m.ListLocalRblEntriesErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*upstream.LocalRblEntry, len(m.LocalRblEntries))
	copy(out, m.LocalRblEntries)
	return out, nil
}

func (m *MockClient) Close() error {
	m.record("Close")
	return m.CloseErr
}
