package systemcontroller

import (
	"context"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// DNSStatus returns the mock DNS status.
func (m *MockClient) DNSStatus(_ context.Context) (*DNSStatusResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "DNSStatus"})

	if m.DNSStatusErr != nil {
		return nil, m.DNSStatusErr
	}

	if m.DNSStatusResp != nil {
		return m.DNSStatusResp, nil
	}

	return &DNSStatusResponse{}, nil
}

// ListDNSRecords returns the mock DNS records.
func (m *MockClient) ListDNSRecords(_ context.Context) ([]*upstream.DnsRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ListDNSRecords"})

	if m.ListDNSRecordsErr != nil {
		return nil, m.ListDNSRecordsErr
	}

	return m.DNSRecords, nil
}

// AddDNSRecord adds a record to the mock.
func (m *MockClient) AddDNSRecord(_ context.Context, record *upstream.DnsRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "AddDNSRecord", Args: []any{record}})

	if m.AddDNSRecordErr != nil {
		return m.AddDNSRecordErr
	}

	m.DNSRecords = append(m.DNSRecords, record)
	return nil
}

// RemoveDNSRecord removes a record from the mock.
func (m *MockClient) RemoveDNSRecord(_ context.Context, name string, recordType *upstream.RecordType) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "RemoveDNSRecord", Args: []any{name, recordType}})

	if m.RemoveDNSRecordErr != nil {
		return 0, m.RemoveDNSRecordErr
	}

	return m.DNSRemoveCount, nil
}

// GetDNSTLD returns the mock TLD.
func (m *MockClient) GetDNSTLD(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "GetDNSTLD"})

	if m.GetDNSTLDErr != nil {
		return "", m.GetDNSTLDErr
	}

	if m.DNSTLD != "" {
		return m.DNSTLD, nil
	}

	return "home", nil
}

// SetDNSTLD sets the mock TLD.
func (m *MockClient) SetDNSTLD(_ context.Context, tld string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "SetDNSTLD", Args: []any{tld}})

	if m.SetDNSTLDErr != nil {
		return m.SetDNSTLDErr
	}

	m.DNSTLD = tld
	return nil
}

// SetupDNS is a no-op mock.
func (m *MockClient) SetupDNS(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "SetupDNS"})

	return m.SetupDNSErr
}
