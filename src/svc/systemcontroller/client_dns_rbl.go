package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// GetRblConfig returns the current RBL (reverse-IP blocklist) configuration.
func (c *SystemdClient) GetRblConfig(ctx context.Context) (_ *RblConfigResponse, err error) {
	resp, err := c.getClient(ctx, "dns/rbl")
	if err != nil {
		return nil, fmt.Errorf("%w: GetRblConfig: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/rbl")
	}

	var result RblConfigResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// SetRblConfig replaces the RBL configuration (enabled flag, provider zones and
// their refusal-code handling).
//
// refusalCooldownSecs is the default number of seconds a provider that refuses
// a query is taken out of the lookup rotation, for providers that set none of
// their own; 0 uses rolodex's built-in default.
func (c *SystemdClient) SetRblConfig(ctx context.Context, enabled bool, providers []RblProviderDTO, refusalCooldownSecs uint32) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RblConfigRequest{
		Enabled:             enabled,
		Providers:           providers,
		RefusalCooldownSecs: refusalCooldownSecs,
	})
	return c.postClient(ctx, "dns/rbl", pr)
}

// GetDnsblConfig returns the current DNSBL (domain blocklist) configuration.
func (c *SystemdClient) GetDnsblConfig(ctx context.Context) (_ *RblConfigResponse, err error) {
	resp, err := c.getClient(ctx, "dns/dnsbl")
	if err != nil {
		return nil, fmt.Errorf("%w: GetDnsblConfig: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/dnsbl")
	}

	var result RblConfigResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// SetDnsblConfig replaces the DNSBL configuration (enabled flag, provider zones
// and their refusal-code handling). The cooldown is independent of the RBL one.
func (c *SystemdClient) SetDnsblConfig(ctx context.Context, enabled bool, providers []RblProviderDTO, refusalCooldownSecs uint32) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RblConfigRequest{
		Enabled:             enabled,
		Providers:           providers,
		RefusalCooldownSecs: refusalCooldownSecs,
	})
	return c.postClient(ctx, "dns/dnsbl", pr)
}

// ListLocalRblEntries returns the local RBL blocklist entries.
func (c *SystemdClient) ListLocalRblEntries(ctx context.Context) (_ []LocalRblEntryDTO, err error) {
	resp, err := c.getClient(ctx, "dns/rbl/local")
	if err != nil {
		return nil, fmt.Errorf("%w: ListLocalRblEntries: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/rbl/local")
	}

	var entries []LocalRblEntryDTO
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

// AddLocalRblEntry adds a name or IP to the local RBL blocklist.
func (c *SystemdClient) AddLocalRblEntry(ctx context.Context, name, reason string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddLocalRblEntryRequest{Name: name, Reason: reason})
	return c.postClient(ctx, "dns/rbl/local/add", pr)
}

// RemoveLocalRblEntry removes a name or IP from the local RBL blocklist.
func (c *SystemdClient) RemoveLocalRblEntry(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemoveLocalRblEntryRequest{Name: name})
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
