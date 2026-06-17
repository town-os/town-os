// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// Client provides access to the ingress gRPC management API. The
// systemcontroller uses it to program routes (the same way it programs rolodex
// DNS records).
type Client interface {
	SetRoutes(ctx context.Context, routes []*ingresspb.Route) error
	AddRoute(ctx context.Context, route *ingresspb.Route) error
	RemoveRoute(ctx context.Context, hostname string) error
	ListRoutes(ctx context.Context) ([]*ingresspb.Route, error)
	Close() error
}

type client struct {
	conn *grpc.ClientConn
	c    ingresspb.IngressClient
}

// Dial connects to an ingress server over its Unix socket and returns a Client.
func Dial(_ context.Context, socketPath string) (Client, error) {
	conn, err := grpc.NewClient(
		"passthrough:///ingress",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(dialCtx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(dialCtx, "unix", socketPath)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("ingress dial: %w", err)
	}
	return &client{conn: conn, c: ingresspb.NewIngressClient(conn)}, nil
}

func (c *client) SetRoutes(ctx context.Context, routes []*ingresspb.Route) error {
	_, err := c.c.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: routes})
	return err
}

func (c *client) AddRoute(ctx context.Context, route *ingresspb.Route) error {
	_, err := c.c.AddRoute(ctx, &ingresspb.AddRouteRequest{Route: route})
	return err
}

func (c *client) RemoveRoute(ctx context.Context, hostname string) error {
	_, err := c.c.RemoveRoute(ctx, &ingresspb.RemoveRouteRequest{Hostname: hostname})
	return err
}

func (c *client) ListRoutes(ctx context.Context) ([]*ingresspb.Route, error) {
	resp, err := c.c.ListRoutes(ctx, &ingresspb.ListRoutesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetRoutes(), nil
}

func (c *client) Close() error {
	return c.conn.Close()
}
