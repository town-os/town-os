package rolodex

import (
	"context"
	"fmt"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// DefaultScopeTTL is the association TTL (seconds) used when binding an overlay
// address to a network scope. Town OS re-binds on every reconcile so a long TTL
// is safe.
const DefaultScopeTTL = 3600

// EnsureNetworkScope idempotently creates a rolodex network scope named name
// with the given home domain (e.g. "office.home."). Existing scopes with the
// same name are left untouched. Callers treat failures as non-fatal: scope
// support may lag on older rolodex servers while the multi-DNS work lands.
func EnsureNetworkScope(ctx context.Context, c Client, name, homeDomain string) error {
	scopes, err := c.ListNetworkScopes(ctx)
	if err != nil {
		return fmt.Errorf("list network scopes: %w", err)
	}
	for _, s := range scopes {
		if s.Name == name {
			return nil
		}
	}
	if err := c.CreateNetworkScope(ctx, &upstream.NetworkScope{Name: name, HomeDomain: homeDomain}); err != nil {
		return fmt.Errorf("create network scope %q: %w", name, err)
	}
	return nil
}

// BindOverlayAddress associates an overlay IP address with a network scope so
// rolodex resolves the scope's records for queries originating from that
// address. This mirrors the box's WireGuard address into rolodex's IPAM view.
func BindOverlayAddress(ctx context.Context, c Client, ipAddress, scopeName string) error {
	if err := c.JoinNetwork(ctx, ipAddress, scopeName, DefaultScopeTTL); err != nil {
		return fmt.Errorf("bind overlay address %s to scope %q: %w", ipAddress, scopeName, err)
	}
	return nil
}
