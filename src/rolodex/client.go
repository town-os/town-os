package rolodex

import (
	"context"
	"fmt"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// Client provides access to the Rolodex gRPC management API.
type Client interface {
	AddRecord(ctx context.Context, record *upstream.DnsRecord) error
	RemoveRecord(ctx context.Context, name string, opts *upstream.RemoveRecordOptions) (uint32, error)
	ListRecords(ctx context.Context, opts *upstream.ListRecordsOptions) ([]*upstream.DnsRecord, error)
	AddAuthoritativeZone(ctx context.Context, zone string) error
	RemoveAuthoritativeZone(ctx context.Context, zone string) error
	ListAuthoritativeZones(ctx context.Context) ([]string, error)
	FlushDnsCache(ctx context.Context) error

	// RBL (Realtime Blackhole List, reverse-IP) provider configuration.
	//
	// refusalCooldownSecs is how long a provider that answers with a refusal
	// code — "you queried via a public resolver", "you are over your query
	// limit" — is taken out of the lookup rotation, for providers that set no
	// value of their own; 0 uses rolodex's built-in default. A refusal is not a
	// listing, and believing one NXDOMAINs every name checked against that
	// provider, so the provider is backed off rather than believed.
	SetRblConfig(ctx context.Context, enabled bool, providers []*upstream.RblConfig, refusalCooldownSecs uint32) error
	GetRblConfig(ctx context.Context) (*upstream.RblStatus, error)

	// DNSBL (domain blocklist, forward-name) provider configuration. The
	// refusal cooldown is independent of the RBL one.
	SetDnsblConfig(ctx context.Context, enabled bool, providers []*upstream.DnsblConfig, refusalCooldownSecs uint32) error
	GetDnsblConfig(ctx context.Context) (*upstream.DnsblStatus, error)

	// Local RBL blocklist entries (DB-backed names/IPs, checked before
	// external providers; a name entry blocks forward domain lookups).
	AddLocalRblEntry(ctx context.Context, entry *upstream.LocalRblEntry) error
	RemoveLocalRblEntry(ctx context.Context, name string) error
	ListLocalRblEntries(ctx context.Context) ([]*upstream.LocalRblEntry, error)

	// DNSBL allowlist entries exempt a name — and every name beneath it —
	// from the name-based blocklist step entirely, overriding both the
	// configured DNSBL providers and any matching local RBL entry. This is
	// the operator's escape hatch from a third-party feed's false positive:
	// without it the only remedy is to disable the whole provider.
	AddDnsblAllowlistEntry(ctx context.Context, entry *upstream.DnsblAllowlistEntry) error
	RemoveDnsblAllowlistEntry(ctx context.Context, name string) error
	ListDnsblAllowlistEntries(ctx context.Context) ([]*upstream.DnsblAllowlistEntry, error)

	// Network scopes group DNS records and IP associations under a named,
	// per-network view. Town OS pairs each network with a scope so records
	// can eventually be resolved per-network; for now scoping is best-effort
	// and non-fatal (server support may lag). See scope.go for helpers.
	CreateNetworkScope(ctx context.Context, scope *upstream.NetworkScope) error
	DeleteNetworkScope(ctx context.Context, name string) error
	ListNetworkScopes(ctx context.Context) ([]*upstream.NetworkScope, error)
	JoinNetwork(ctx context.Context, ipAddress, scopeName string, ttlSeconds uint64) error
	LeaveNetwork(ctx context.Context, ipAddress string) error
	GetNetworkAssociations(ctx context.Context, scopeName string) ([]*upstream.NetworkAssociation, error)
	AddScopedRecord(ctx context.Context, scopeName string, record *upstream.DnsRecord) error
	RemoveScopedRecord(ctx context.Context, scopeName, name string, opts *upstream.RemoveScopedRecordOptions) (uint32, error)
	ListScopedRecords(ctx context.Context, scopeName string, opts *upstream.ListScopedRecordsOptions) ([]*upstream.DnsRecord, error)

	// Owned TLDs partition the DNS namespace per network: a name under a
	// scope's TLD resolves only within that network and is never forwarded
	// upstream. A scope's home_domain is its implicit primary TLD; these
	// register/inspect additional ones. Per-TLD forwarders are the overlay
	// addresses of other network members running rolodex, consulted for names
	// under the TLD before an authoritative NXDOMAIN. See scope.go for helpers.
	AddScopeTld(ctx context.Context, scopeName, tld string) error
	// AddScopeTldWithListener additionally binds a DNS listener (UDP+TCP) to
	// listenIP on the server. This is how rolodex comes to answer on a network's
	// overlay address at all — the listener is a full resolver, and the ingress
	// answer-rewriting is confined to names under tld. See EnsureScopeListener.
	AddScopeTldWithListener(ctx context.Context, scopeName, tld, listenIP string) error
	RemoveScopeTld(ctx context.Context, scopeName, tld string) error
	ListScopeTlds(ctx context.Context, scopeName string) ([]string, error)
	SetScopeTldForwarders(ctx context.Context, scopeName, tld string, forwarders []string) error
	ListScopeTldForwarders(ctx context.Context, scopeName, tld string) ([]string, error)

	Close() error
}

// client wraps the upstream rolodex Go client.
type client struct {
	c *upstream.Client
}

// Dial connects to a Rolodex server via Unix socket and returns a Client.
func Dial(ctx context.Context, socketPath string) (c Client, err error) {
	uc, err := upstream.Dial(ctx, socketPath, upstream.WithUnixSocket())
	if err != nil {
		return nil, fmt.Errorf("rolodex dial: %w", err)
	}
	return &client{c: uc}, nil
}

func (c *client) AddRecord(ctx context.Context, record *upstream.DnsRecord) error {
	return c.c.AddRecord(ctx, record)
}

func (c *client) RemoveRecord(ctx context.Context, name string, opts *upstream.RemoveRecordOptions) (uint32, error) {
	return c.c.RemoveRecord(ctx, name, opts)
}

func (c *client) ListRecords(ctx context.Context, opts *upstream.ListRecordsOptions) ([]*upstream.DnsRecord, error) {
	return c.c.ListRecords(ctx, opts)
}

func (c *client) AddAuthoritativeZone(ctx context.Context, zone string) error {
	return c.c.AddAuthoritativeZone(ctx, zone)
}

func (c *client) RemoveAuthoritativeZone(ctx context.Context, zone string) error {
	return c.c.RemoveAuthoritativeZone(ctx, zone)
}

func (c *client) ListAuthoritativeZones(ctx context.Context) ([]string, error) {
	return c.c.ListAuthoritativeZones(ctx)
}

func (c *client) FlushDnsCache(ctx context.Context) error {
	return c.c.FlushDnsCache(ctx)
}

func (c *client) SetRblConfig(ctx context.Context, enabled bool, providers []*upstream.RblConfig, refusalCooldownSecs uint32) error {
	return c.c.SetRblConfigWithRefusalCooldown(ctx, enabled, providers, refusalCooldownSecs)
}

func (c *client) GetRblConfig(ctx context.Context) (*upstream.RblStatus, error) {
	return c.c.GetRblConfig(ctx)
}

func (c *client) SetDnsblConfig(ctx context.Context, enabled bool, providers []*upstream.DnsblConfig, refusalCooldownSecs uint32) error {
	return c.c.SetDnsblConfigWithRefusalCooldown(ctx, enabled, providers, refusalCooldownSecs)
}

func (c *client) GetDnsblConfig(ctx context.Context) (*upstream.DnsblStatus, error) {
	return c.c.GetDnsblConfig(ctx)
}

func (c *client) AddLocalRblEntry(ctx context.Context, entry *upstream.LocalRblEntry) error {
	return c.c.AddLocalRblEntry(ctx, entry)
}

func (c *client) RemoveLocalRblEntry(ctx context.Context, name string) error {
	return c.c.RemoveLocalRblEntry(ctx, name)
}

func (c *client) ListLocalRblEntries(ctx context.Context) ([]*upstream.LocalRblEntry, error) {
	return c.c.ListLocalRblEntries(ctx)
}

func (c *client) AddDnsblAllowlistEntry(ctx context.Context, entry *upstream.DnsblAllowlistEntry) error {
	return c.c.AddDnsblAllowlistEntry(ctx, entry)
}

func (c *client) RemoveDnsblAllowlistEntry(ctx context.Context, name string) error {
	return c.c.RemoveDnsblAllowlistEntry(ctx, name)
}

func (c *client) ListDnsblAllowlistEntries(ctx context.Context) ([]*upstream.DnsblAllowlistEntry, error) {
	return c.c.ListDnsblAllowlistEntries(ctx)
}

func (c *client) CreateNetworkScope(ctx context.Context, scope *upstream.NetworkScope) error {
	return c.c.CreateNetworkScope(ctx, scope)
}

func (c *client) DeleteNetworkScope(ctx context.Context, name string) error {
	return c.c.DeleteNetworkScope(ctx, name)
}

func (c *client) ListNetworkScopes(ctx context.Context) ([]*upstream.NetworkScope, error) {
	return c.c.ListNetworkScopes(ctx)
}

func (c *client) JoinNetwork(ctx context.Context, ipAddress, scopeName string, ttlSeconds uint64) error {
	return c.c.JoinNetwork(ctx, ipAddress, scopeName, ttlSeconds)
}

func (c *client) LeaveNetwork(ctx context.Context, ipAddress string) error {
	return c.c.LeaveNetwork(ctx, ipAddress)
}

func (c *client) GetNetworkAssociations(ctx context.Context, scopeName string) ([]*upstream.NetworkAssociation, error) {
	return c.c.GetNetworkAssociations(ctx, scopeName)
}

func (c *client) AddScopedRecord(ctx context.Context, scopeName string, record *upstream.DnsRecord) error {
	return c.c.AddScopedRecord(ctx, scopeName, record)
}

func (c *client) RemoveScopedRecord(ctx context.Context, scopeName, name string, opts *upstream.RemoveScopedRecordOptions) (uint32, error) {
	return c.c.RemoveScopedRecord(ctx, scopeName, name, opts)
}

func (c *client) ListScopedRecords(ctx context.Context, scopeName string, opts *upstream.ListScopedRecordsOptions) ([]*upstream.DnsRecord, error) {
	return c.c.ListScopedRecords(ctx, scopeName, opts)
}

func (c *client) AddScopeTld(ctx context.Context, scopeName, tld string) error {
	return c.c.AddScopeTld(ctx, scopeName, tld)
}

func (c *client) AddScopeTldWithListener(ctx context.Context, scopeName, tld, listenIP string) error {
	return c.c.AddScopeTldWithListener(ctx, scopeName, tld, listenIP)
}

func (c *client) RemoveScopeTld(ctx context.Context, scopeName, tld string) error {
	return c.c.RemoveScopeTld(ctx, scopeName, tld)
}

func (c *client) ListScopeTlds(ctx context.Context, scopeName string) ([]string, error) {
	return c.c.ListScopeTlds(ctx, scopeName)
}

func (c *client) SetScopeTldForwarders(ctx context.Context, scopeName, tld string, forwarders []string) error {
	return c.c.SetScopeTldForwarders(ctx, scopeName, tld, forwarders)
}

func (c *client) ListScopeTldForwarders(ctx context.Context, scopeName, tld string) ([]string, error) {
	return c.c.ListScopeTldForwarders(ctx, scopeName, tld)
}

func (c *client) Close() error {
	return c.c.Close()
}
