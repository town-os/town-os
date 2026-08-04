// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"errors"
	"slices"
	"sort"
)

// Grants an account can hold.
//
// A named set rather than a boolean per capability, so adding the next one is a
// single entry in AllGrants plus the routes it guards — not a new column, a new
// migration, a new UpdateFields pointer, and a new checkbox wired through four
// files. Two exist today.
const (
	// GrantWireGuard permits enrolling and refreshing WireGuard peers on the
	// account's networks.
	GrantWireGuard = "wireguard"
	// GrantGfeh permits administering the object storage those same networks
	// own: the partition's principals, their grants, and its published links.
	GrantGfeh = "gfeh"
)

// AllGrants is every grant that exists, in the order the UI should offer them.
//
// This is the registry. A grant absent from it cannot be stored (ValidGrant
// refuses it), which is what stops a typo in an API request from becoming a
// permission that silently never matches anything.
var AllGrants = []string{GrantWireGuard, GrantGfeh}

var (
	// ErrInvalidGrant is returned when a grant name is not in AllGrants.
	ErrInvalidGrant = errors.New("unknown grant")
	// ErrGrantsAdmin is returned when explicit grants are stored on an
	// administrator. An administrator holds every grant already, so a stored
	// subset could only ever disagree with that — and whichever check ran
	// first would decide what the account meant.
	ErrGrantsAdmin = errors.New("an administrator already holds every grant")
	// ErrGrantsNoNetworks is returned when an account holds grants but is
	// scoped to no network. Every grant is exercised against a network, so
	// such an account could authenticate and do nothing — and an empty scope
	// must never be read as "every network".
	ErrGrantsNoNetworks = errors.New("an account holding grants must be scoped to at least one network")
)

// ValidGrant reports whether a name is a grant this build knows.
func ValidGrant(name string) bool { return slices.Contains(AllGrants, name) }

// validateGrants checks every name is known.
func validateGrants(grants []string) error {
	for _, g := range grants {
		if !ValidGrant(g) {
			return ErrInvalidGrant
		}
	}
	return nil
}

// normalizeGrants de-duplicates and orders a grant set so the stored value is
// canonical regardless of the order the caller supplied — the same treatment
// the network scope gets, and for the same reason: an unchanged set must render
// unchanged bytes or every comparison downstream reports a spurious change.
func normalizeGrants(grants []string) []string {
	seen := make(map[string]struct{}, len(grants))
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// HoldsEveryGrant reports whether the account's authority is unconditional.
//
// An administrator holds every grant on every network, which is what makes every
// capability check in the control plane a question about non-admins only. Named
// rather than open-coded as `acct.Admin` at each site so the rule has one
// statement.
func (a Account) HoldsEveryGrant() bool { return a.Admin }

// HasGrant reports whether the account may exercise a named grant.
//
// An administrator has every one, including any added after this account was
// created — which is the whole point of asking here rather than reading Grants
// directly.
func (a Account) HasGrant(name string) bool {
	if a.HoldsEveryGrant() {
		return true
	}
	return slices.Contains(a.Grants, name)
}

// Restricted reports whether the account is confined to the routes its grants
// unlock.
//
// Derived, never stored: it is simply a non-admin holding at least one grant.
// There is no account "kind" — an account is an administrator (every grant) or
// it is not, and a non-admin carries whatever grants are toggled on. An account
// with no grants is an ordinary dashboard user and is governed by the ordinary
// per-route auth; one that holds grants exists to exercise them and reaches
// nothing else, which is what grantAllowlist enforces.
func (a Account) Restricted() bool { return !a.Admin && len(a.Grants) > 0 }

// MayAdministerNetwork reports whether the account may act on a named network.
//
// Scope only: whether it may do the particular thing is HasGrant. An
// administrator may act on any network, including one created after the account
// was; anybody else is confined to their stored scope.
//
// An empty scope denies everything rather than permitting everything. The
// account layer refuses to store a grant-holding account without one, so this
// is belt-and-braces for a row that arrived some other way — but it is the one
// place where getting the direction wrong hands out every network on the box.
func (a Account) MayAdministerNetwork(network string) bool {
	if a.HoldsEveryGrant() {
		return true
	}
	return slices.Contains(a.Networks, network)
}
