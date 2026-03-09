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
	FlushDnsCache(ctx context.Context) error
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

func (c *client) FlushDnsCache(ctx context.Context) error {
	return c.c.FlushDnsCache(ctx)
}

func (c *client) Close() error {
	return c.c.Close()
}
