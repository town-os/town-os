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

// SetRblConfig replaces the RBL configuration (enabled flag + provider zones).
func (c *SystemdClient) SetRblConfig(ctx context.Context, enabled bool, providers []RblProviderDTO) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RblConfigRequest{Enabled: enabled, Providers: providers})
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

// SetDnsblConfig replaces the DNSBL configuration (enabled flag + provider zones).
func (c *SystemdClient) SetDnsblConfig(ctx context.Context, enabled bool, providers []RblProviderDTO) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RblConfigRequest{Enabled: enabled, Providers: providers})
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

// ListBlocklists returns the curated blocklist catalog and current apply status.
func (c *SystemdClient) ListBlocklists(ctx context.Context) (_ *BlocklistsResponse, err error) {
	resp, err := c.getClient(ctx, "dns/blocklists")
	if err != nil {
		return nil, fmt.Errorf("%w: ListBlocklists: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "dns/blocklists")
	}

	var result BlocklistsResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// ApplyBlocklists starts a background apply of the given curated feeds (or a
// custom feed). It returns the keys of the feeds whose apply was started.
func (c *SystemdClient) ApplyBlocklists(ctx context.Context, req ApplyBlocklistsRequest) (_ []string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, req)

	resp, err := c.postJSON(ctx, "dns/blocklists/apply", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ApplyBlocklists: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusAccepted {
		return nil, readProblemDetail(resp, "POST", "dns/blocklists/apply")
	}

	var result struct {
		Feeds []string `json:"feeds"`
	}
	return result.Feeds, json.NewDecoder(resp.Body).Decode(&result)
}

// ClearBlocklists removes local RBL entries that originated from blocklist
// feeds. Empty keys clears all blocklist-sourced entries. Returns the count.
func (c *SystemdClient) ClearBlocklists(ctx context.Context, keys []string) (_ int, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ClearBlocklistsRequest{Keys: keys})

	resp, err := c.postJSON(ctx, "dns/blocklists/clear", pr)
	if err != nil {
		return 0, fmt.Errorf("%w: ClearBlocklists: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return 0, readProblemDetail(resp, "POST", "dns/blocklists/clear")
	}

	var result struct {
		Removed int `json:"removed"`
	}
	return result.Removed, json.NewDecoder(resp.Body).Decode(&result)
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
