package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// GetDnsblConfig returns the current DNSBL (domain blocklist) configuration.
func (c *SystemdClient) GetDnsblConfig(ctx context.Context) (_ *BlocklistConfigResponse, err error) {
	resp, err := c.getClient(ctx, "dns/dnsbl")
	if err != nil {
		return nil, fmt.Errorf("%w: GetDnsblConfig: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/dnsbl")
	}

	var result BlocklistConfigResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// SetDnsblConfig replaces the DNSBL configuration (enabled flag, provider zones
// and their refusal-code handling).
func (c *SystemdClient) SetDnsblConfig(ctx context.Context, enabled bool, providers []BlocklistProviderDTO, refusalCooldownSecs uint32) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, BlocklistConfigRequest{
		Enabled:             enabled,
		Providers:           providers,
		RefusalCooldownSecs: refusalCooldownSecs,
	})
	return c.postClient(ctx, "dns/dnsbl", pr)
}

// ListLocalBlocklistEntries returns the local blocklist entries.
func (c *SystemdClient) ListLocalBlocklistEntries(ctx context.Context) (_ []LocalBlocklistEntryDTO, err error) {
	resp, err := c.getClient(ctx, "dns/rbl/local")
	if err != nil {
		return nil, fmt.Errorf("%w: ListLocalBlocklistEntries: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/rbl/local")
	}

	var entries []LocalBlocklistEntryDTO
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

// AddLocalBlocklistEntry adds a name or IP to the local blocklist.
func (c *SystemdClient) AddLocalBlocklistEntry(ctx context.Context, name, reason string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddLocalBlocklistEntryRequest{Name: name, Reason: reason})
	return c.postClient(ctx, "dns/rbl/local/add", pr)
}

// RemoveLocalBlocklistEntry removes a name or IP from the local blocklist.
func (c *SystemdClient) RemoveLocalBlocklistEntry(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemoveLocalBlocklistEntryRequest{Name: name})
	return c.postClient(ctx, "dns/rbl/local/remove", pr)
}

// ListDnsblAllowlistEntries returns the DNSBL allowlist entries.
func (c *SystemdClient) ListDnsblAllowlistEntries(ctx context.Context) (_ []DnsblAllowlistEntryDTO, err error) {
	resp, err := c.getClient(ctx, "dns/dnsbl/allowlist")
	if err != nil {
		return nil, fmt.Errorf("%w: ListDnsblAllowlistEntries: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/dnsbl/allowlist")
	}

	var entries []DnsblAllowlistEntryDTO
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

// AddDnsblAllowlistEntry exempts a name (and every name beneath it) from the
// name-based blocklist check.
func (c *SystemdClient) AddDnsblAllowlistEntry(ctx context.Context, name, reason string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddDnsblAllowlistEntryRequest{Name: name, Reason: reason})
	return c.postClient(ctx, "dns/dnsbl/allowlist/add", pr)
}

// RemoveDnsblAllowlistEntry removes a name from the DNSBL allowlist.
func (c *SystemdClient) RemoveDnsblAllowlistEntry(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemoveDnsblAllowlistEntryRequest{Name: name})
	return c.postClient(ctx, "dns/dnsbl/allowlist/remove", pr)
}

// ListDNSServices returns installed package services with their published state.
func (c *SystemdClient) ListDNSServices(ctx context.Context) (_ []DNSServiceEntry, err error) {
	resp, err := c.getClient(ctx, "dns/services")
	if err != nil {
		return nil, fmt.Errorf("%w: ListDNSServices: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/services")
	}

	var entries []DNSServiceEntry
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

// SetDNSService publishes or unpublishes a package service in the DNS zone.
func (c *SystemdClient) SetDNSService(ctx context.Context, repo, name string, published bool) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetDNSServiceRequest{Repo: repo, Name: name, Published: published})
	return c.postClient(ctx, "dns/services/set", pr)
}
