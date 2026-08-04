// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

// Grants are named toggles on an ordinary account. There is no account "kind":
// an account is an administrator — holding every grant, including ones added
// after it was created — or it is not, and a non-admin carries whichever grants
// are switched on.

// --- The registry ---

// Adding a grant must be one entry in AllGrants. These pin the two that exist
// and that the set is the authority ValidGrant answers from, so a third arrives
// working rather than needing a second list updated somewhere else.
func TestAllGrantsIsTheRegistry(t *testing.T) {
	for _, grant := range []string{GrantWireGuard, GrantGfeh} {
		if !slices.Contains(AllGrants, grant) {
			t.Errorf("AllGrants omits %q: %v", grant, AllGrants)
		}
		if !ValidGrant(grant) {
			t.Errorf("ValidGrant(%q) = false", grant)
		}
	}
	if ValidGrant("not-a-grant") {
		t.Error("ValidGrant accepted a name that is not in the registry")
	}
	if ValidGrant("") {
		t.Error("ValidGrant accepted the empty string")
	}
}

// A name outside the registry is refused rather than stored. A typo that became
// a permission would match no route and fail silently forever.
func TestUnknownGrantIsRefused(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			_, err := mgr.m.CreateGranted("portal", "password123", "p@example.com", "555-1000", "Portal",
				[]string{"wireguard", "storage-admin"}, []string{"office"})
			if !errors.Is(err, ErrInvalidGrant) {
				t.Fatalf("CreateGranted with an unknown grant = %v, want ErrInvalidGrant", err)
			}

			if _, err := mgr.m.Create("bob", "password123", "b@example.com", "555-1000", "Bob", false); err != nil {
				t.Fatalf("Create: %v", err)
			}
			bad := []string{"nonsense"}
			if _, err := mgr.m.Update("bob", UpdateFields{Grants: &bad}); !errors.Is(err, ErrInvalidGrant) {
				t.Fatalf("Update with an unknown grant = %v, want ErrInvalidGrant", err)
			}
		})
	}
}

// --- Who holds what ---

// An administrator holds every grant, including any added after this account
// existed — which is why callers ask HasGrant rather than reading Grants.
func TestAdminHoldsEveryGrant(t *testing.T) {
	admin := Account{Username: "root", Admin: true}
	if !admin.HoldsEveryGrant() {
		t.Error("an administrator must hold every grant")
	}
	for _, grant := range append(slices.Clone(AllGrants), "a-grant-added-tomorrow") {
		if !admin.HasGrant(grant) {
			t.Errorf("an administrator must hold %q", grant)
		}
	}
	if admin.Restricted() {
		t.Error("an administrator must not be confined by the allowlist")
	}
}

// A non-admin holds exactly the grants toggled on — the point of a toggle.
func TestGrantsAreIndependentToggles(t *testing.T) {
	wg := Account{Username: "portal", Grants: []string{GrantWireGuard}, Networks: []string{"office"}}
	if !wg.HasGrant(GrantWireGuard) {
		t.Error("the wireguard grant did not take")
	}
	if wg.HasGrant(GrantGfeh) {
		t.Error("the wireguard grant leaked into gfeh")
	}

	obj := Account{Username: "shelf", Grants: []string{GrantGfeh}, Networks: []string{"office"}}
	if !obj.HasGrant(GrantGfeh) {
		t.Error("the gfeh grant did not take")
	}
	if obj.HasGrant(GrantWireGuard) {
		t.Error("the gfeh grant leaked into wireguard")
	}

	both := Account{Username: "both", Grants: AllGrants, Networks: []string{"office"}}
	for _, grant := range AllGrants {
		if !both.HasGrant(grant) {
			t.Errorf("an account granted everything is missing %q", grant)
		}
	}
}

// An account with no grants is an ordinary dashboard user: it holds none, and
// the fail-closed allowlist does not confine it.
func TestPlainAccountHoldsNoGrants(t *testing.T) {
	plain := Account{Username: "bob"}
	for _, grant := range AllGrants {
		if plain.HasGrant(grant) {
			t.Errorf("a plain account holds %q", grant)
		}
	}
	if plain.Restricted() {
		t.Error("a plain account must not be confined by the allowlist")
	}
	if plain.MayAdministerNetwork("home") {
		t.Error("a plain account must administer no network")
	}
}

// Holding a grant is what confines an account to the routes it unlocks.
func TestGrantHolderIsRestricted(t *testing.T) {
	acct := Account{Username: "portal", Grants: []string{GrantGfeh}, Networks: []string{"office"}}
	if !acct.Restricted() {
		t.Error("an account holding a grant must be confined to the routes it unlocks")
	}
}

// --- Scope ---

// Scope answers "which network", never "what may I do". An administrator is on
// every network including one created after the account was.
func TestScopeIsSeparateFromGrant(t *testing.T) {
	acct := Account{Username: "portal", Grants: AllGrants, Networks: []string{"office", "lab"}}
	for _, network := range []string{"office", "lab"} {
		if !acct.MayAdministerNetwork(network) {
			t.Errorf("scoped account denied on %q", network)
		}
	}
	for _, network := range []string{"home", "other", ""} {
		if acct.MayAdministerNetwork(network) {
			t.Errorf("scoped account permitted on %q", network)
		}
	}

	admin := Account{Username: "root", Admin: true}
	if !admin.MayAdministerNetwork("a-network-created-tomorrow") {
		t.Error("an administrator must be permitted on any network")
	}
}

// An empty scope denies everything rather than permitting everything. Every
// other layer refuses to store such a row; this is the one place where the
// wrong direction hands out every network on the box.
func TestEmptyScopeIsNeverEveryNetwork(t *testing.T) {
	acct := Account{Username: "broken", Grants: AllGrants}
	for _, network := range []string{"home", "office", ""} {
		if acct.MayAdministerNetwork(network) {
			t.Errorf("an empty scope permitted %q", network)
		}
	}
}

// --- Storage invariants, in both implementations ---

// A grant is exercised against a network, so an account holding one must be
// scoped to at least one.
func TestGrantsRequireANetworkScope(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			if _, err := mgr.m.CreateGranted("portal", "password123", "p@example.com", "555-1000", "Portal",
				[]string{GrantGfeh}, nil); !errors.Is(err, ErrGrantsNoNetworks) {
				t.Fatalf("CreateGranted with no scope = %v, want ErrGrantsNoNetworks", err)
			}

			// Create scopes an ordinary account to the home network as
			// membership, so the unscoped state has to be reached explicitly.
			// Clearing it is legal while the account holds no grants — the
			// emptiness rule is only about grant-holding accounts.
			if _, err := mgr.m.Create("bob", "password123", "b@example.com", "555-1000", "Bob", false); err != nil {
				t.Fatalf("Create: %v", err)
			}
			var none []string
			if _, err := mgr.m.Update("bob", UpdateFields{Networks: &none}); err != nil {
				t.Fatalf("clearing the scope of an account holding no grants: %v", err)
			}
			grants := []string{GrantWireGuard}
			if _, err := mgr.m.Update("bob", UpdateFields{Grants: &grants}); !errors.Is(err, ErrGrantsNoNetworks) {
				t.Fatalf("granting with no scope = %v, want ErrGrantsNoNetworks", err)
			}
		})
	}
}

// Clearing the scope out from under an existing grant is the same violation
// arriving from the other side, and is caught by resolving the post-update row.
func TestClearingScopeUnderAGrantIsRefused(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			if _, err := mgr.m.CreateGranted("portal", "password123", "p@example.com", "555-1000", "Portal",
				[]string{GrantGfeh}, []string{"office"}); err != nil {
				t.Fatalf("CreateGranted: %v", err)
			}
			var none []string
			if _, err := mgr.m.Update("portal", UpdateFields{Networks: &none}); !errors.Is(err, ErrGrantsNoNetworks) {
				t.Fatalf("clearing the scope = %v, want ErrGrantsNoNetworks", err)
			}
		})
	}
}

// An administrator holds every grant already, so a stored subset could only
// ever disagree with that.
func TestGrantsOnAnAdministratorAreRefused(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			if _, err := mgr.m.Create("root", "password123", "r@example.com", "555-1000", "Root", true); err != nil {
				t.Fatalf("Create admin: %v", err)
			}
			grants := []string{GrantGfeh}
			networks := []string{"office"}
			if _, err := mgr.m.Update("root", UpdateFields{Grants: &grants, Networks: &networks}); !errors.Is(err, ErrGrantsAdmin) {
				t.Fatalf("granting an administrator = %v, want ErrGrantsAdmin", err)
			}

			// And the refusal left the row alone rather than half-applying.
			got, err := mgr.m.Get("root")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if len(got.Grants) != 0 {
				t.Errorf("the rejected update was applied anyway: %v", got.Grants)
			}
		})
	}
}

// The same conflict from the other side: promoting a grant-holder.
func TestPromotingAGrantHolderIsRefused(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			if _, err := mgr.m.CreateGranted("portal", "password123", "p@example.com", "555-1000", "Portal",
				[]string{GrantWireGuard}, []string{"office"}); err != nil {
				t.Fatalf("CreateGranted: %v", err)
			}
			admin := true
			if _, err := mgr.m.Update("portal", UpdateFields{Admin: &admin}); !errors.Is(err, ErrGrantsAdmin) {
				t.Fatalf("promoting a grant holder = %v, want ErrGrantsAdmin", err)
			}
		})
	}
}

// A grant-holder is never an admin by construction, whatever the caller wants.
func TestCreateGrantedIsNeverAdmin(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			acct, err := mgr.m.CreateGranted("portal", "password123", "p@example.com", "555-1000", "Portal",
				[]string{GrantWireGuard}, []string{"office"})
			if err != nil {
				t.Fatalf("CreateGranted: %v", err)
			}
			if acct.Admin {
				t.Error("CreateGranted produced an administrator")
			}
		})
	}
}

// --- Round trip ---

// Grants survive storage, come back canonical, and toggle off again. Dropping
// every grant and clearing the scope in one call is legal: the result is an
// ordinary account, and rejecting it would leave no way to demote anybody.
func TestGrantsRoundTripAndToggleOff(t *testing.T) {
	for _, mgr := range grantManagers(t) {
		t.Run(mgr.name, func(t *testing.T) {
			if _, err := mgr.m.CreateGranted("portal", "password123", "p@example.com", "555-1000", "Portal",
				[]string{GrantGfeh, GrantWireGuard, GrantGfeh}, []string{"office"}); err != nil {
				t.Fatalf("CreateGranted: %v", err)
			}

			got, err := mgr.m.Get("portal")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			// De-duplicated and ordered, so an unchanged set renders unchanged
			// bytes and nothing downstream reports a spurious change.
			if want := []string{GrantGfeh, GrantWireGuard}; !reflect.DeepEqual(got.Grants, want) {
				t.Errorf("grants = %v, want %v", got.Grants, want)
			}

			one := []string{GrantWireGuard}
			after, err := mgr.m.Update("portal", UpdateFields{Grants: &one})
			if err != nil {
				t.Fatalf("Update(narrow): %v", err)
			}
			if after.HasGrant(GrantGfeh) {
				t.Error("a revoked grant is still held")
			}
			if !after.HasGrant(GrantWireGuard) {
				t.Error("the surviving grant was dropped")
			}

			var none []string
			plain, err := mgr.m.Update("portal", UpdateFields{Grants: &none, Networks: &none})
			if err != nil {
				t.Fatalf("Update(drop all): %v", err)
			}
			if len(plain.Grants) != 0 {
				t.Errorf("grants survived being cleared: %v", plain.Grants)
			}
			if plain.Restricted() {
				t.Error("a demoted account is still confined")
			}
		})
	}
}

// --- Both implementations must agree ---

type namedManager struct {
	name string
	m    Manager
}

// grantManagers returns a fresh SQLite manager and a fresh mock, so every rule
// above is asserted against both. The mock is what handler tests run against;
// one that permitted a state the real store refuses would let those tests pass
// against a row production can never hold.
func grantManagers(t *testing.T) []namedManager {
	t.Helper()
	return []namedManager{
		{name: "sqlite", m: initTestDB(t)},
		{name: "mock", m: InitMockManager()},
	}
}
