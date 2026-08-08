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

	// Network scope state.
	Scopes        []*upstream.NetworkScope
	ScopedRecords map[string][]*upstream.DnsRecord
	Associations  map[string]string // ipAddress -> scopeName

	// Scope TLD state.
	ScopeTlds          map[string][]string // scopeName -> additional owned TLDs
	ScopeTldForwarders map[string][]string // scopeName + "\x00" + tld -> forwarders
	ScopeTldListeners  map[string]string   // scopeName + "\x00" + tld -> listen IP

	// RBL / DNSBL state.
	RblEnabled      bool
	RblProviders    []*upstream.RblConfig
	DnsblEnabled    bool
	DnsblProviders  []*upstream.DnsblConfig
	LocalRblEntries []*upstream.LocalRblEntry

	// Refusal handling: how long a provider that refused a query stays out of
	// the lookup rotation, and which providers are currently out. The cooldowns
	// are recorded from the Set calls; the rotated-out lists are set by the test
	// (a mock has no lookup path to refuse anything).
	RblRefusalCooldownSecs   uint32
	DnsblRefusalCooldownSecs uint32
	RblRotatedOut            []*upstream.RotatedProvider
	DnsblRotatedOut          []*upstream.RotatedProvider

	DnsblAllowlistEntries []*upstream.DnsblAllowlistEntry

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

	AddDnsblAllowlistEntryErr    error
	RemoveDnsblAllowlistEntryErr error
	ListDnsblAllowlistEntriesErr error

	CreateNetworkScopeErr     error
	DeleteNetworkScopeErr     error
	ListNetworkScopesErr      error
	JoinNetworkErr            error
	LeaveNetworkErr           error
	GetNetworkAssociationsErr error
	AddScopedRecordErr        error
	RemoveScopedRecordErr     error
	ListScopedRecordsErr      error
	AddScopeTldErr            error
	RemoveScopeTldErr         error
	AddScopeTldListenerErr    error
	ListScopeTldsErr          error
	SetScopeTldForwardersErr  error
	ListScopeTldForwardersErr error
}

// scopeTldKey builds the ScopeTldForwarders map key for a (scope, tld) pair.
func scopeTldKey(scope, tld string) string {
	return scope + "\x00" + tld
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

func (m *MockClient) SetRblConfig(_ context.Context, enabled bool, providers []*upstream.RblConfig, refusalCooldownSecs uint32) error {
	m.record("SetRblConfig", enabled, providers, refusalCooldownSecs)
	if m.SetRblConfigErr != nil {
		return m.SetRblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RblEnabled = enabled
	m.RblProviders = slices.Clone(providers)
	m.RblRefusalCooldownSecs = refusalCooldownSecs
	return nil
}

func (m *MockClient) GetRblConfig(_ context.Context) (*upstream.RblStatus, error) {
	m.record("GetRblConfig")
	if m.GetRblConfigErr != nil {
		return nil, m.GetRblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return &upstream.RblStatus{
		Enabled:             m.RblEnabled,
		Providers:           slices.Clone(m.RblProviders),
		RefusalCooldownSecs: m.RblRefusalCooldownSecs,
		RotatedOut:          slices.Clone(m.RblRotatedOut),
	}, nil
}

func (m *MockClient) SetDnsblConfig(_ context.Context, enabled bool, providers []*upstream.DnsblConfig, refusalCooldownSecs uint32) error {
	m.record("SetDnsblConfig", enabled, providers, refusalCooldownSecs)
	if m.SetDnsblConfigErr != nil {
		return m.SetDnsblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DnsblEnabled = enabled
	m.DnsblProviders = slices.Clone(providers)
	m.DnsblRefusalCooldownSecs = refusalCooldownSecs
	return nil
}

func (m *MockClient) GetDnsblConfig(_ context.Context) (*upstream.DnsblStatus, error) {
	m.record("GetDnsblConfig")
	if m.GetDnsblConfigErr != nil {
		return nil, m.GetDnsblConfigErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return &upstream.DnsblStatus{
		Enabled:             m.DnsblEnabled,
		Providers:           slices.Clone(m.DnsblProviders),
		RefusalCooldownSecs: m.DnsblRefusalCooldownSecs,
		RotatedOut:          slices.Clone(m.DnsblRotatedOut),
	}, nil
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

func (m *MockClient) AddDnsblAllowlistEntry(_ context.Context, entry *upstream.DnsblAllowlistEntry) error {
	m.record("AddDnsblAllowlistEntry", entry)
	if m.AddDnsblAllowlistEntryErr != nil {
		return m.AddDnsblAllowlistEntryErr
	}
	if entry == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.DnsblAllowlistEntries {
		if e.Name == entry.Name {
			e.Reason = entry.Reason
			return nil
		}
	}
	m.DnsblAllowlistEntries = append(m.DnsblAllowlistEntries, entry)
	return nil
}

func (m *MockClient) RemoveDnsblAllowlistEntry(_ context.Context, name string) error {
	m.record("RemoveDnsblAllowlistEntry", name)
	if m.RemoveDnsblAllowlistEntryErr != nil {
		return m.RemoveDnsblAllowlistEntryErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.DnsblAllowlistEntries {
		if e.Name == name {
			m.DnsblAllowlistEntries = append(m.DnsblAllowlistEntries[:i], m.DnsblAllowlistEntries[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MockClient) ListDnsblAllowlistEntries(_ context.Context) ([]*upstream.DnsblAllowlistEntry, error) {
	m.record("ListDnsblAllowlistEntries")
	if m.ListDnsblAllowlistEntriesErr != nil {
		return nil, m.ListDnsblAllowlistEntriesErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*upstream.DnsblAllowlistEntry, len(m.DnsblAllowlistEntries))
	copy(out, m.DnsblAllowlistEntries)
	return out, nil
}

func (m *MockClient) CreateNetworkScope(_ context.Context, scope *upstream.NetworkScope) error {
	m.record("CreateNetworkScope", scope)
	if m.CreateNetworkScopeErr != nil {
		return m.CreateNetworkScopeErr
	}
	if scope == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.Scopes {
		if s.Name == scope.Name {
			return nil
		}
	}
	m.Scopes = append(m.Scopes, scope)
	return nil
}

func (m *MockClient) DeleteNetworkScope(_ context.Context, name string) error {
	m.record("DeleteNetworkScope", name)
	if m.DeleteNetworkScopeErr != nil {
		return m.DeleteNetworkScopeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.Scopes {
		if s.Name == name {
			m.Scopes = append(m.Scopes[:i], m.Scopes[i+1:]...)
			break
		}
	}
	delete(m.ScopedRecords, name)
	return nil
}

func (m *MockClient) ListNetworkScopes(_ context.Context) ([]*upstream.NetworkScope, error) {
	m.record("ListNetworkScopes")
	if m.ListNetworkScopesErr != nil {
		return nil, m.ListNetworkScopesErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*upstream.NetworkScope, len(m.Scopes))
	copy(out, m.Scopes)
	return out, nil
}

func (m *MockClient) JoinNetwork(_ context.Context, ipAddress, scopeName string, ttlSeconds uint64) error {
	m.record("JoinNetwork", ipAddress, scopeName, ttlSeconds)
	if m.JoinNetworkErr != nil {
		return m.JoinNetworkErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Associations == nil {
		m.Associations = map[string]string{}
	}
	m.Associations[ipAddress] = scopeName
	return nil
}

func (m *MockClient) LeaveNetwork(_ context.Context, ipAddress string) error {
	m.record("LeaveNetwork", ipAddress)
	if m.LeaveNetworkErr != nil {
		return m.LeaveNetworkErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Associations, ipAddress)
	return nil
}

func (m *MockClient) GetNetworkAssociations(_ context.Context, scopeName string) ([]*upstream.NetworkAssociation, error) {
	m.record("GetNetworkAssociations", scopeName)
	if m.GetNetworkAssociationsErr != nil {
		return nil, m.GetNetworkAssociationsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*upstream.NetworkAssociation
	for ip, scope := range m.Associations {
		if scopeName != "" && scope != scopeName {
			continue
		}
		out = append(out, &upstream.NetworkAssociation{IpAddress: ip, ScopeName: scope})
	}
	return out, nil
}

func (m *MockClient) AddScopedRecord(_ context.Context, scopeName string, record *upstream.DnsRecord) error {
	m.record("AddScopedRecord", scopeName, record)
	if m.AddScopedRecordErr != nil {
		return m.AddScopedRecordErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ScopedRecords == nil {
		m.ScopedRecords = map[string][]*upstream.DnsRecord{}
	}
	m.ScopedRecords[scopeName] = append(m.ScopedRecords[scopeName], record)
	return nil
}

func (m *MockClient) RemoveScopedRecord(_ context.Context, scopeName, name string, opts *upstream.RemoveScopedRecordOptions) (uint32, error) {
	m.record("RemoveScopedRecord", scopeName, name, opts)
	if m.RemoveScopedRecordErr != nil {
		return 0, m.RemoveScopedRecordErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []*upstream.DnsRecord
	var removed uint32
	for _, r := range m.ScopedRecords[scopeName] {
		if r.Name == name && (opts == nil || opts.RecordType == nil || *opts.RecordType == r.RecordType) {
			removed++
		} else {
			kept = append(kept, r)
		}
	}
	if m.ScopedRecords != nil {
		m.ScopedRecords[scopeName] = kept
	}
	return removed, nil
}

func (m *MockClient) ListScopedRecords(_ context.Context, scopeName string, _ *upstream.ListScopedRecordsOptions) ([]*upstream.DnsRecord, error) {
	m.record("ListScopedRecords", scopeName)
	if m.ListScopedRecordsErr != nil {
		return nil, m.ListScopedRecordsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.ScopedRecords[scopeName]
	out := make([]*upstream.DnsRecord, len(src))
	copy(out, src)
	return out, nil
}

func (m *MockClient) AddScopeTld(_ context.Context, scopeName, tld string) error {
	m.record("AddScopeTld", scopeName, tld)
	if m.AddScopeTldErr != nil {
		return m.AddScopeTldErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ScopeTlds == nil {
		m.ScopeTlds = map[string][]string{}
	}
	if !slices.Contains(m.ScopeTlds[scopeName], tld) {
		m.ScopeTlds[scopeName] = append(m.ScopeTlds[scopeName], tld)
	}
	return nil
}

func (m *MockClient) AddScopeTldWithListener(_ context.Context, scopeName, tld, listenIP string) error {
	m.record("AddScopeTldWithListener", scopeName, tld, listenIP)
	if m.AddScopeTldListenerErr != nil {
		return m.AddScopeTldListenerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ScopeTlds == nil {
		m.ScopeTlds = map[string][]string{}
	}
	if !slices.Contains(m.ScopeTlds[scopeName], tld) {
		m.ScopeTlds[scopeName] = append(m.ScopeTlds[scopeName], tld)
	}
	if listenIP != "" {
		if m.ScopeTldListeners == nil {
			m.ScopeTldListeners = map[string]string{}
		}
		m.ScopeTldListeners[scopeName+"\x00"+tld] = listenIP
	}
	return nil
}

func (m *MockClient) RemoveScopeTld(_ context.Context, scopeName, tld string) error {
	m.record("RemoveScopeTld", scopeName, tld)
	if m.RemoveScopeTldErr != nil {
		return m.RemoveScopeTldErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ScopeTlds != nil {
		m.ScopeTlds[scopeName] = slices.DeleteFunc(m.ScopeTlds[scopeName], func(t string) bool {
			return t == tld
		})
	}
	return nil
}

func (m *MockClient) ListScopeTlds(_ context.Context, scopeName string) ([]string, error) {
	m.record("ListScopeTlds", scopeName)
	if m.ListScopeTldsErr != nil {
		return nil, m.ListScopeTldsErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.ScopeTlds[scopeName]
	out := make([]string, len(src))
	copy(out, src)
	return out, nil
}

func (m *MockClient) SetScopeTldForwarders(_ context.Context, scopeName, tld string, forwarders []string) error {
	m.record("SetScopeTldForwarders", scopeName, tld, forwarders)
	if m.SetScopeTldForwardersErr != nil {
		return m.SetScopeTldForwardersErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ScopeTldForwarders == nil {
		m.ScopeTldForwarders = map[string][]string{}
	}
	key := scopeTldKey(scopeName, tld)
	if len(forwarders) == 0 {
		delete(m.ScopeTldForwarders, key)
	} else {
		cp := make([]string, len(forwarders))
		copy(cp, forwarders)
		m.ScopeTldForwarders[key] = cp
	}
	return nil
}

func (m *MockClient) ListScopeTldForwarders(_ context.Context, scopeName, tld string) ([]string, error) {
	m.record("ListScopeTldForwarders", scopeName, tld)
	if m.ListScopeTldForwardersErr != nil {
		return nil, m.ListScopeTldForwardersErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.ScopeTldForwarders[scopeTldKey(scopeName, tld)]
	out := make([]string, len(src))
	copy(out, src)
	return out, nil
}

func (m *MockClient) Close() error {
	m.record("Close")
	return m.CloseErr
}
