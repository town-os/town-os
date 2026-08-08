package systemcontroller

import "context"

// GetRblConfig returns the mock RBL configuration.
func (m *MockClient) GetRblConfig(_ context.Context) (*RblConfigResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetRblConfig"})
	if m.GetRblConfigErr != nil {
		return nil, m.GetRblConfigErr
	}
	if m.RblConfig != nil {
		return m.RblConfig, nil
	}
	return &RblConfigResponse{}, nil
}

// SetRblConfig records the RBL configuration on the mock.
func (m *MockClient) SetRblConfig(_ context.Context, enabled bool, providers []RblProviderDTO, refusalCooldownSecs uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetRblConfig", Args: []any{enabled, providers, refusalCooldownSecs}})
	if m.SetRblConfigErr != nil {
		return m.SetRblConfigErr
	}
	m.RblConfig = &RblConfigResponse{
		Enabled:             enabled,
		Providers:           providers,
		RefusalCooldownSecs: refusalCooldownSecs,
	}
	return nil
}

// GetDnsblConfig returns the mock DNSBL configuration.
func (m *MockClient) GetDnsblConfig(_ context.Context) (*RblConfigResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetDnsblConfig"})
	if m.GetDnsblConfigErr != nil {
		return nil, m.GetDnsblConfigErr
	}
	if m.DnsblConfig != nil {
		return m.DnsblConfig, nil
	}
	return &RblConfigResponse{}, nil
}

// SetDnsblConfig records the DNSBL configuration on the mock.
func (m *MockClient) SetDnsblConfig(_ context.Context, enabled bool, providers []RblProviderDTO, refusalCooldownSecs uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetDnsblConfig", Args: []any{enabled, providers, refusalCooldownSecs}})
	if m.SetDnsblConfigErr != nil {
		return m.SetDnsblConfigErr
	}
	m.DnsblConfig = &RblConfigResponse{
		Enabled:             enabled,
		Providers:           providers,
		RefusalCooldownSecs: refusalCooldownSecs,
	}
	return nil
}

// ListLocalRblEntries returns the mock local RBL entries.
func (m *MockClient) ListLocalRblEntries(_ context.Context) ([]LocalRblEntryDTO, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListLocalRblEntries"})
	if m.ListLocalRblErr != nil {
		return nil, m.ListLocalRblErr
	}
	return m.LocalRblEntries, nil
}

// AddLocalRblEntry adds an entry to the mock local RBL list.
func (m *MockClient) AddLocalRblEntry(_ context.Context, name, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddLocalRblEntry", Args: []any{name, reason}})
	if m.AddLocalRblErr != nil {
		return m.AddLocalRblErr
	}
	m.LocalRblEntries = append(m.LocalRblEntries, LocalRblEntryDTO{Name: name, Reason: reason})
	return nil
}

// RemoveLocalRblEntry removes an entry from the mock local RBL list.
func (m *MockClient) RemoveLocalRblEntry(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveLocalRblEntry", Args: []any{name}})
	if m.RemoveLocalRblErr != nil {
		return m.RemoveLocalRblErr
	}
	for i, e := range m.LocalRblEntries {
		if e.Name == name {
			m.LocalRblEntries = append(m.LocalRblEntries[:i], m.LocalRblEntries[i+1:]...)
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
