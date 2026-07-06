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
	SetRblConfig(ctx context.Context, enabled bool, providers []*upstream.RblConfig) error
	GetRblConfig(ctx context.Context) (*upstream.RblStatus, error)

	// DNSBL (domain blocklist, forward-name) provider configuration.
	SetDnsblConfig(ctx context.Context, enabled bool, providers []*upstream.DnsblConfig) error
	GetDnsblConfig(ctx context.Context) (*upstream.DnsblStatus, error)

	// Local RBL blocklist entries (DB-backed names/IPs, checked before
	// external providers; a name entry blocks forward domain lookups).
	AddLocalRblEntry(ctx context.Context, entry *upstream.LocalRblEntry) error
	RemoveLocalRblEntry(ctx context.Context, name string) error
	ListLocalRblEntries(ctx context.Context) ([]*upstream.LocalRblEntry, error)

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

func (c *client) SetRblConfig(ctx context.Context, enabled bool, providers []*upstream.RblConfig) error {
	return c.c.SetRblConfig(ctx, enabled, providers)
}

func (c *client) GetRblConfig(ctx context.Context) (*upstream.RblStatus, error) {
	return c.c.GetRblConfig(ctx)
}

func (c *client) SetDnsblConfig(ctx context.Context, enabled bool, providers []*upstream.DnsblConfig) error {
	return c.c.SetDnsblConfig(ctx, enabled, providers)
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

func (c *client) Close() error {
	return c.c.Close()
}
