package rolodex

import (
	"context"
	"fmt"
	"slices"

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

// EnsureScopeTld idempotently registers tld as an additional owned TLD for a
// scope. A network's primary TLD is already carried as the scope's home_domain
// by EnsureNetworkScope, so this is only needed for any *extra* TLDs a network
// serves. Registering a TLD already owned by the same scope is a no-op; a TLD
// owned by a different scope returns an error (owned TLDs are globally unique).
func EnsureScopeTld(ctx context.Context, c Client, scopeName, tld string) error {
	existing, err := c.ListScopeTlds(ctx, scopeName)
	if err != nil {
		return fmt.Errorf("list scope tlds for %q: %w", scopeName, err)
	}
	if slices.Contains(existing, tld) {
		return nil
	}
	if err := c.AddScopeTld(ctx, scopeName, tld); err != nil {
		return fmt.Errorf("add tld %q to scope %q: %w", tld, scopeName, err)
	}
	return nil
}

// EnsureScopeListener makes rolodex LISTEN on the box's overlay address for a
// network, so peers on the WireGuard overlay can actually use it as their
// resolver. The peer configs we hand out set `DNS = <overlay .1>`; without this
// nothing is bound there and a peer's query lands on a closed port.
//
// BindOverlayAddress is NOT this: it is JoinNetwork, a source-IP -> scope
// association in rolodex's IPAM, which only decides *how* a query is answered
// once it arrives. This is what makes it arrive.
//
// rolodex has no "bind an address" RPC; the one dynamic-bind mechanism is the
// per-TLD ingress listener attached via AddScopeTld's listen_ip, which spawns a
// full UDP+TCP resolver on that IP (and is replayed from the database at boot,
// so it survives a restart). The ingress answer-rewriting it also enables is
// confined to names under tld — a peer's query for anything else recurses
// normally — so the listener doubles as the overlay's general resolver.
//
// tld is the network's TLD (its scope home_domain). Registering a scope's own
// home domain as an owned TLD is idempotent server-side (INSERT OR IGNORE, and
// a conflict is only raised for a TLD owned by a DIFFERENT scope), so this is
// safe to call on every reconcile — and must be, since the listener has to be
// re-asserted if the overlay address changes.
func EnsureScopeListener(ctx context.Context, c Client, scopeName, tld, listenIP string) error {
	if err := c.AddScopeTldWithListener(ctx, scopeName, tld, listenIP); err != nil {
		return fmt.Errorf("bind dns listener %s for scope %q tld %q: %w", listenIP, scopeName, tld, err)
	}
	return nil
}

// ReconcileTldForwarders makes the scope's per-TLD peer forwarder set equal
// addrs (a replace-all set). Forwarders are the overlay addresses of other
// network members running rolodex; rolodex consults them for names under the
// scope's TLD before returning an authoritative NXDOMAIN.
func ReconcileTldForwarders(ctx context.Context, c Client, scopeName, tld string, addrs []string) error {
	if err := c.SetScopeTldForwarders(ctx, scopeName, tld, addrs); err != nil {
		return fmt.Errorf("set tld forwarders for scope %q tld %q: %w", scopeName, tld, err)
	}
	return nil
}
