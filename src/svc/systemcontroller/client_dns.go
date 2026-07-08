package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// DNSStatus returns the current state of the DNS service.
func (c *SystemdClient) DNSStatus(ctx context.Context) (_ *DNSStatusResponse, err error) {
	resp, err := c.getClient(ctx, "dns/status")
	if err != nil {
		return nil, fmt.Errorf("%w: DNSStatus: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/status")
	}

	var result DNSStatusResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// ListDNSRecords returns DNS records annotated with their network and TLD. With
// an empty tld it returns records across every network (global + scoped); a
// non-empty tld restricts the result to that domain.
func (c *SystemdClient) ListDNSRecords(ctx context.Context, tld string) (_ []*DNSRecordView, err error) {
	path := "dns/records"
	if tld != "" {
		path += "?tld=" + url.QueryEscape(tld)
	}
	resp, err := c.getClient(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w: ListDNSRecords: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/records")
	}

	var records []*DNSRecordView
	return records, json.NewDecoder(resp.Body).Decode(&records)
}

// AddDNSRecord adds a DNS record via the rolodex service.
func (c *SystemdClient) AddDNSRecord(ctx context.Context, record *upstream.DnsRecord) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddDNSRecordRequest{
		Name:       record.Name,
		RecordType: record.RecordType,
		Value:      record.Value,
		TTL:        record.Ttl,
	})

	return c.postClient(ctx, "dns/records/add", pr)
}

// RemoveDNSRecord removes DNS records matching the given name and optional type.
func (c *SystemdClient) RemoveDNSRecord(ctx context.Context, name string, recordType *upstream.RecordType) (_ uint32, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemoveDNSRecordRequest{
		Name:       name,
		RecordType: recordType,
	})

	resp, err := c.postJSON(ctx, "dns/records/remove", pr)
	if err != nil {
		return 0, fmt.Errorf("%w: RemoveDNSRecord: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, readProblemDetail(resp, "POST", "dns/records/remove")
	}

	var result struct {
		Removed uint32 `json:"removed"`
	}
	return result.Removed, json.NewDecoder(resp.Body).Decode(&result)
}

// GetDNSTLD returns the current TLD setting.
func (c *SystemdClient) GetDNSTLD(ctx context.Context) (_ string, err error) {
	resp, err := c.getClient(ctx, "dns/tld")
	if err != nil {
		return "", fmt.Errorf("%w: GetDNSTLD: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return "", readProblemDetail(resp, "GET", "dns/tld")
	}

	var result struct {
		TLD string `json:"tld"`
	}
	return result.TLD, json.NewDecoder(resp.Body).Decode(&result)
}

// SetDNSTLD changes the TLD and re-provisions all DNS records.
func (c *SystemdClient) SetDNSTLD(ctx context.Context, tld string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetTLDRequest{TLD: tld})

	return c.postClient(ctx, "dns/tld", pr)
}

// SetupDNS initializes or reconciles the TLD zone and all package DNS records.
func (c *SystemdClient) SetupDNS(ctx context.Context) error {
	return c.postClient(ctx, "dns/setup", nil)
}
