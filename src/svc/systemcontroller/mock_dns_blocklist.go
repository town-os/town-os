package systemcontroller

import "context"

// GetDnsblConfig returns the mock DNSBL configuration.
func (m *MockClient) GetDnsblConfig(_ context.Context) (*BlocklistConfigResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetDnsblConfig"})
	if m.GetDnsblConfigErr != nil {
		return nil, m.GetDnsblConfigErr
	}
	if m.DnsblConfig != nil {
		return m.DnsblConfig, nil
	}
	return &BlocklistConfigResponse{}, nil
}

// SetDnsblConfig records the DNSBL configuration on the mock.
func (m *MockClient) SetDnsblConfig(_ context.Context, enabled bool, providers []BlocklistProviderDTO, refusalCooldownSecs uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetDnsblConfig", Args: []any{enabled, providers, refusalCooldownSecs}})
	if m.SetDnsblConfigErr != nil {
		return m.SetDnsblConfigErr
	}
	m.DnsblConfig = &BlocklistConfigResponse{
		Enabled:             enabled,
		Providers:           providers,
		RefusalCooldownSecs: refusalCooldownSecs,
	}
	return nil
}

// ListLocalBlocklistEntries returns the mock local blocklist entries.
func (m *MockClient) ListLocalBlocklistEntries(_ context.Context) ([]LocalBlocklistEntryDTO, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListLocalBlocklistEntries"})
	if m.ListLocalBlocklistErr != nil {
		return nil, m.ListLocalBlocklistErr
	}
	return m.LocalBlocklistEntries, nil
}

// AddLocalBlocklistEntry adds an entry to the mock local RBL list.
func (m *MockClient) AddLocalBlocklistEntry(_ context.Context, name, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddLocalBlocklistEntry", Args: []any{name, reason}})
	if m.AddLocalBlocklistErr != nil {
		return m.AddLocalBlocklistErr
	}
	m.LocalBlocklistEntries = append(m.LocalBlocklistEntries, LocalBlocklistEntryDTO{Name: name, Reason: reason})
	return nil
}

// RemoveLocalBlocklistEntry removes an entry from the mock local RBL list.
func (m *MockClient) RemoveLocalBlocklistEntry(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveLocalBlocklistEntry", Args: []any{name}})
	if m.RemoveLocalBlocklistErr != nil {
		return m.RemoveLocalBlocklistErr
	}
	for i, e := range m.LocalBlocklistEntries {
		if e.Name == name {
			m.LocalBlocklistEntries = append(m.LocalBlocklistEntries[:i], m.LocalBlocklistEntries[i+1:]...)
			break
		}
	}
	return nil
}

// ListDnsblAllowlistEntries returns the mock DNSBL allowlist entries.
func (m *MockClient) ListDnsblAllowlistEntries(_ context.Context) ([]DnsblAllowlistEntryDTO, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListDnsblAllowlistEntries"})
	if m.ListDnsblAllowlistErr != nil {
		return nil, m.ListDnsblAllowlistErr
	}
	return m.DnsblAllowlistEntries, nil
}

// AddDnsblAllowlistEntry adds an entry to the mock DNSBL allowlist.
func (m *MockClient) AddDnsblAllowlistEntry(_ context.Context, name, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddDnsblAllowlistEntry", Args: []any{name, reason}})
	if m.AddDnsblAllowlistErr != nil {
		return m.AddDnsblAllowlistErr
	}
	m.DnsblAllowlistEntries = append(m.DnsblAllowlistEntries, DnsblAllowlistEntryDTO{Name: name, Reason: reason})
	return nil
}

// RemoveDnsblAllowlistEntry removes an entry from the mock DNSBL allowlist.
func (m *MockClient) RemoveDnsblAllowlistEntry(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveDnsblAllowlistEntry", Args: []any{name}})
	if m.RemoveDnsblAllowlistErr != nil {
		return m.RemoveDnsblAllowlistErr
	}
	for i, e := range m.DnsblAllowlistEntries {
		if e.Name == name {
			m.DnsblAllowlistEntries = append(m.DnsblAllowlistEntries[:i], m.DnsblAllowlistEntries[i+1:]...)
			break
		}
	}
	return nil
}

// ListDNSServices returns the mock DNS service entries.
func (m *MockClient) ListDNSServices(_ context.Context) ([]DNSServiceEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListDNSServices"})
	if m.ListDNSServicesErr != nil {
		return nil, m.ListDNSServicesErr
	}
	return m.DNSServices, nil
}

// SetDNSService records a publish/unpublish request and updates mock state.
func (m *MockClient) SetDNSService(_ context.Context, repo, name string, published bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetDNSService", Args: []any{repo, name, published}})
	if m.SetDNSServiceErr != nil {
		return m.SetDNSServiceErr
	}
	for i := range m.DNSServices {
		if m.DNSServices[i].Repo == repo && m.DNSServices[i].Name == name {
			m.DNSServices[i].Published = published
		}
	}
	return nil
}
