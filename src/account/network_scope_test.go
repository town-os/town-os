// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// --- CreateNetworkOnly ---

func TestCreateNetworkOnlyAccount(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555-1000", "Portal", AllGrants, []string{"office"})
	if err != nil {
		t.Fatalf("CreateNetworkOnly: %v", err)
	}
	if !acct.Restricted() {
		t.Error("expected WireGuard=true")
	}
	if acct.Admin {
		t.Error("a wireguard account must never be admin")
	}
	if !reflect.DeepEqual(acct.Networks, []string{"office"}) {
		t.Errorf("Networks = %v, want [office]", acct.Networks)
	}

	// Persisted and read back identically.
	got, err := mgr.Get(t.Context(), "portal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Restricted() || !reflect.DeepEqual(got.Networks, []string{"office"}) {
		t.Errorf("round-trip mismatch: restricted=%v Networks=%v", got.Restricted(), got.Networks)
	}
}

func TestCreateNetworkOnlyRejectsEmptyScope(t *testing.T) {
	mgr := initTestDB(t)

	for _, networks := range [][]string{nil, {}} {
		_, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555-1000", "Portal", AllGrants, networks)
		if !errors.Is(err, ErrGrantsNoNetworks) {
			t.Errorf("CreateNetworkOnly(%v) error = %v, want ErrGrantsNoNetworks", networks, err)
		}
	}
	// Nothing should have been written.
	if _, err := mgr.Get(t.Context(), "portal"); !errors.Is(err, ErrNotFound) {
		t.Errorf("account should not exist after a rejected create, got %v", err)
	}
}

func TestCreateNetworkOnlyRejectsInvalidNetworkName(t *testing.T) {
	mgr := initTestDB(t)

	_, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555-1000", "Portal", AllGrants, []string{"office", "Not A Network"})
	if !errors.Is(err, ErrInvalidNetworkName) {
		t.Errorf("error = %v, want ErrInvalidNetworkName", err)
	}
}

func TestCreateNetworkOnlyNormalizesScope(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555-1000", "Portal", AllGrants, []string{"office", "home-lab", "office"}) // duplicate + unsorted
	if err != nil {
		t.Fatalf("CreateNetworkOnly: %v", err)
	}
	if want := []string{"home-lab", "office"}; !reflect.DeepEqual(acct.Networks, want) {
		t.Errorf("Networks = %v, want %v (deduped + sorted)", acct.Networks, want)
	}
}

func TestCreateNetworkOnlyStillValidatesContactAndPassword(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.CreateGranted(t.Context(), "p", "short", "p@example.com", "555", "P", AllGrants, []string{"office"}); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: got %v, want ErrPasswordTooShort", err)
	}
	if _, err := mgr.CreateGranted(t.Context(), "p", "password123", "", "555", "P", AllGrants, []string{"office"}); !errors.Is(err, ErrMissingContactInfo) {
		t.Errorf("missing email: got %v, want ErrMissingContactInfo", err)
	}
}

// --- Normal accounts are unaffected ---

// A plain account holds no grants and is a member of the home network -- the
// one network that always exists.
//
// Membership is not confinement, and the difference is the whole reason this is
// safe: the scope column confines only a restricted account (Restricted() is
// false here), so nothing about what this account may reach follows from it.
// What it buys is that an ordinary account is on the box's network from the
// moment it is created, including the very first one.
func TestPlainCreateJoinsTheHomeNetwork(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.Create(t.Context(), "alice", "password123", "a@example.com", "555", "Alice", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if acct.Restricted() {
		t.Error("a plain account must not be confined by the allowlist")
	}
	if !reflect.DeepEqual(acct.Networks, []string{DefaultNetworkName}) {
		t.Errorf("Networks = %v, want [%s]", acct.Networks, DefaultNetworkName)
	}

	got, err := mgr.Get(t.Context(), "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Restricted() || !reflect.DeepEqual(got.Networks, []string{DefaultNetworkName}) {
		t.Errorf("plain account read back as restricted=%v Networks=%v", got.Restricted(), got.Networks)
	}
}

// The first account on the box is created through the same path, before boot
// reconcile has run -- so the network it is scoped to has to exist already,
// which is what InitNetworkManager's seed guarantees.
func TestFirstAdminJoinsTheHomeNetwork(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.Create(t.Context(), "root", "password123", "r@example.com", "555", "Root", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !reflect.DeepEqual(acct.Networks, []string{DefaultNetworkName}) {
		t.Errorf("Networks = %v, want [%s]", acct.Networks, DefaultNetworkName)
	}
	// It changes nothing about the administrator's authority, which is
	// unconditional and never consults the scope.
	if !acct.HoldsEveryGrant() || !acct.MayAdministerNetwork("a-network-created-tomorrow") {
		t.Error("an administrator's authority must not follow from its scope")
	}
}

// --- Update ---




// --- List round-trips the scope ---

func TestListReturnsNetworkScope(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.Create(t.Context(), "alice", "password123", "a@example.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555", "Portal", AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateNetworkOnly: %v", err)
	}

	accts, err := mgr.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]Account{}
	for _, a := range accts {
		byName[a.Username] = a
	}
	if byName["portal"].Restricted() != true || !reflect.DeepEqual(byName["portal"].Networks, []string{"office"}) {
		t.Errorf("portal in List: restricted=%v Networks=%v", byName["portal"].Restricted(), byName["portal"].Networks)
	}
	if byName["alice"].Restricted() {
		t.Error("alice should not be WireGuard in List")
	}
}

// --- Authenticate surfaces the scope (the allowlist middleware needs it) ---

func TestAuthenticateReturnsNetworkScope(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555", "Portal", AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateNetworkOnly: %v", err)
	}

	acct, err := mgr.Authenticate(t.Context(), "portal", "password123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !acct.Restricted() || !reflect.DeepEqual(acct.Networks, []string{"office"}) {
		t.Errorf("Authenticate scope: restricted=%v Networks=%v", acct.Restricted(), acct.Networks)
	}
}

// --- Migration from a pre-network-only database ---

// TestMigrationFromPreNetworkOnlyDB proves that a database created before the
// network_only/networks columns existed is transparently upgraded, and its rows
// read back as ordinary accounts.
func TestMigrationFromPreNetworkOnlyDB(t *testing.T) {
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	// The exact pre-migration schema, with a row inserted through it.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE accounts (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		email         TEXT NOT NULL,
		phone         TEXT NOT NULL,
		real_name     TEXT NOT NULL,
		admin         INTEGER NOT NULL DEFAULT 0,
		disabled      INTEGER NOT NULL DEFAULT 0,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create old accounts table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (username, password_hash, email, phone, real_name, admin, disabled, created_at, updated_at)
		 VALUES ('legacy', 'x', 'l@example.com', '555', 'Legacy', 1, 0, '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	// InitManager must add the missing columns without touching the row's data.
	mgr, err := InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager (migration): %v", err)
	}

	got, err := mgr.Get(t.Context(), "legacy")
	if err != nil {
		t.Fatalf("Get legacy: %v", err)
	}
	if !got.Admin {
		t.Error("legacy admin flag was lost across migration")
	}
	if got.Restricted() {
		t.Error("legacy row should default to NetworkOnly=false")
	}
	if len(got.Networks) != 0 {
		t.Errorf("legacy row should default to empty scope, got %v", got.Networks)
	}

	// And the migrated table now accepts a network-only account.
	if _, err := mgr.CreateGranted(t.Context(), "portal", "password123", "p@example.com", "555", "Portal", AllGrants, []string{"office"}); err != nil {
		t.Fatalf("CreateNetworkOnly on migrated DB: %v", err)
	}
}

// TestInitManagerMigrationIsIdempotent runs InitManager twice on the same DB;
// the second run must not error on the already-present columns.
func TestInitManagerMigrationIsIdempotent(t *testing.T) {
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "twice.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	if _, err := InitManager(t.Context(), db); err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	if _, err := InitManager(t.Context(), db); err != nil {
		t.Fatalf("InitManager second (idempotent): %v", err)
	}
}

// --- normalizeNetworkScope / validateNetworkScope units ---

func TestNormalizeNetworkScope(t *testing.T) {
	got := normalizeNetworkScope([]string{"z", "a", "z", "m", "a"})
	if want := []string{"a", "m", "z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("normalizeNetworkScope = %v, want %v", got, want)
	}
}

func TestValidateNetworkScope(t *testing.T) {
	if err := validateNetworkScope(nil); !errors.Is(err, ErrGrantsNoNetworks) {
		t.Errorf("empty: got %v, want ErrGrantsNoNetworks", err)
	}
	if err := validateNetworkScope([]string{"ok", "BAD NAME"}); !errors.Is(err, ErrInvalidNetworkName) {
		t.Errorf("bad name: got %v, want ErrInvalidNetworkName", err)
	}
	if err := validateNetworkScope([]string{"office", "home-lab"}); err != nil {
		t.Errorf("valid scope: got %v, want nil", err)
	}
}
