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

// --- CreateWireGuard ---

func TestCreateWireGuardAccount(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555-1000", "Portal", []string{"office"})
	if err != nil {
		t.Fatalf("CreateWireGuard: %v", err)
	}
	if !acct.WireGuard {
		t.Error("expected WireGuard=true")
	}
	if acct.Admin {
		t.Error("a wireguard account must never be admin")
	}
	if !reflect.DeepEqual(acct.Networks, []string{"office"}) {
		t.Errorf("Networks = %v, want [office]", acct.Networks)
	}

	// Persisted and read back identically.
	got, err := mgr.Get("portal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.WireGuard || !reflect.DeepEqual(got.Networks, []string{"office"}) {
		t.Errorf("round-trip mismatch: WireGuard=%v Networks=%v", got.WireGuard, got.Networks)
	}
}

func TestCreateWireGuardRejectsEmptyScope(t *testing.T) {
	mgr := initTestDB(t)

	for _, networks := range [][]string{nil, {}} {
		_, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555-1000", "Portal", networks)
		if !errors.Is(err, ErrWireGuardNoNetworks) {
			t.Errorf("CreateWireGuard(%v) error = %v, want ErrWireGuardNoNetworks", networks, err)
		}
	}
	// Nothing should have been written.
	if _, err := mgr.Get("portal"); !errors.Is(err, ErrNotFound) {
		t.Errorf("account should not exist after a rejected create, got %v", err)
	}
}

func TestCreateWireGuardRejectsInvalidNetworkName(t *testing.T) {
	mgr := initTestDB(t)

	_, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555-1000", "Portal",
		[]string{"office", "Not A Network"})
	if !errors.Is(err, ErrInvalidNetworkName) {
		t.Errorf("error = %v, want ErrInvalidNetworkName", err)
	}
}

func TestCreateWireGuardNormalizesScope(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555-1000", "Portal",
		[]string{"office", "home-lab", "office"}) // duplicate + unsorted
	if err != nil {
		t.Fatalf("CreateWireGuard: %v", err)
	}
	if want := []string{"home-lab", "office"}; !reflect.DeepEqual(acct.Networks, want) {
		t.Errorf("Networks = %v, want %v (deduped + sorted)", acct.Networks, want)
	}
}

func TestCreateWireGuardStillValidatesContactAndPassword(t *testing.T) {
	mgr := initTestDB(t)

	if _, err := mgr.CreateWireGuard("p", "short", "p@example.com", "555", "P", []string{"office"}); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: got %v, want ErrPasswordTooShort", err)
	}
	if _, err := mgr.CreateWireGuard("p", "password123", "", "555", "P", []string{"office"}); !errors.Is(err, ErrMissingContactInfo) {
		t.Errorf("missing email: got %v, want ErrMissingContactInfo", err)
	}
}

// --- Normal accounts are unaffected ---

func TestPlainCreateHasNoWireGuardScope(t *testing.T) {
	mgr := initTestDB(t)

	acct, err := mgr.Create("alice", "password123", "a@example.com", "555", "Alice", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if acct.WireGuard {
		t.Error("plain account should have WireGuard=false")
	}
	if len(acct.Networks) != 0 {
		t.Errorf("plain account should have no networks, got %v", acct.Networks)
	}

	got, err := mgr.Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.WireGuard || len(got.Networks) != 0 {
		t.Errorf("plain account read back as WireGuard=%v Networks=%v", got.WireGuard, got.Networks)
	}
}

// --- Update ---

func TestUpdateTogglesWireGuard(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.Create("alice", "password123", "a@example.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Turn WireGuard on, supplying a scope in the same update.
	on := true
	nets := []string{"office"}
	acct, err := mgr.Update("alice", UpdateFields{WireGuard: &on, Networks: &nets})
	if err != nil {
		t.Fatalf("Update on: %v", err)
	}
	if !acct.WireGuard || !reflect.DeepEqual(acct.Networks, []string{"office"}) {
		t.Errorf("after enable: WireGuard=%v Networks=%v", acct.WireGuard, acct.Networks)
	}

	// Change the scope.
	nets2 := []string{"home-lab", "office"}
	acct, err = mgr.Update("alice", UpdateFields{Networks: &nets2})
	if err != nil {
		t.Fatalf("Update scope: %v", err)
	}
	if !reflect.DeepEqual(acct.Networks, []string{"home-lab", "office"}) {
		t.Errorf("after rescope: Networks=%v", acct.Networks)
	}

	// Turn WireGuard off; scope becomes irrelevant and may be emptied.
	off := false
	empty := []string{}
	acct, err = mgr.Update("alice", UpdateFields{WireGuard: &off, Networks: &empty})
	if err != nil {
		t.Fatalf("Update off: %v", err)
	}
	if acct.WireGuard {
		t.Error("expected WireGuard=false after disable")
	}
}

func TestUpdateWireGuardOnWithoutNetworksRejected(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.Create("alice", "password123", "a@example.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	on := true
	_, err := mgr.Update("alice", UpdateFields{WireGuard: &on})
	if !errors.Is(err, ErrWireGuardNoNetworks) {
		t.Fatalf("enabling WireGuard with no scope: got %v, want ErrWireGuardNoNetworks", err)
	}

	// The rejected update must not have partially applied.
	got, err := mgr.Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.WireGuard {
		t.Error("account was left WireGuard=true after a rejected update")
	}
}

func TestUpdateClearingScopeOnWireGuardAccountRejected(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555", "Portal", []string{"office"}); err != nil {
		t.Fatalf("CreateWireGuard: %v", err)
	}

	empty := []string{}
	_, err := mgr.Update("portal", UpdateFields{Networks: &empty})
	if !errors.Is(err, ErrWireGuardNoNetworks) {
		t.Fatalf("clearing scope on a WireGuard account: got %v, want ErrWireGuardNoNetworks", err)
	}

	// Original scope must survive the rejected update intact.
	got, err := mgr.Get("portal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got.Networks, []string{"office"}) {
		t.Errorf("scope corrupted by rejected update: %v", got.Networks)
	}
}

// --- List round-trips the scope ---

func TestListReturnsWireGuardScope(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.Create("alice", "password123", "a@example.com", "555", "Alice", false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555", "Portal", []string{"office"}); err != nil {
		t.Fatalf("CreateWireGuard: %v", err)
	}

	accts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]Account{}
	for _, a := range accts {
		byName[a.Username] = a
	}
	if byName["portal"].WireGuard != true || !reflect.DeepEqual(byName["portal"].Networks, []string{"office"}) {
		t.Errorf("portal in List: WireGuard=%v Networks=%v", byName["portal"].WireGuard, byName["portal"].Networks)
	}
	if byName["alice"].WireGuard {
		t.Error("alice should not be WireGuard in List")
	}
}

// --- Authenticate surfaces the scope (the allowlist middleware needs it) ---

func TestAuthenticateReturnsWireGuardScope(t *testing.T) {
	mgr := initTestDB(t)
	if _, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555", "Portal", []string{"office"}); err != nil {
		t.Fatalf("CreateWireGuard: %v", err)
	}

	acct, err := mgr.Authenticate("portal", "password123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !acct.WireGuard || !reflect.DeepEqual(acct.Networks, []string{"office"}) {
		t.Errorf("Authenticate scope: WireGuard=%v Networks=%v", acct.WireGuard, acct.Networks)
	}
}

// --- Migration from a pre-WireGuard database ---

// TestMigrationFromPreWireGuardDB proves that a database created before the
// wireguard/networks columns existed is transparently upgraded, and its rows
// read back as ordinary non-WireGuard accounts.
func TestMigrationFromPreWireGuardDB(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "old.db"))
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
	mgr, err := InitManager(db)
	if err != nil {
		t.Fatalf("InitManager (migration): %v", err)
	}

	got, err := mgr.Get("legacy")
	if err != nil {
		t.Fatalf("Get legacy: %v", err)
	}
	if !got.Admin {
		t.Error("legacy admin flag was lost across migration")
	}
	if got.WireGuard {
		t.Error("legacy row should default to WireGuard=false")
	}
	if len(got.Networks) != 0 {
		t.Errorf("legacy row should default to empty scope, got %v", got.Networks)
	}

	// And the migrated table now accepts a WireGuard account.
	if _, err := mgr.CreateWireGuard("portal", "password123", "p@example.com", "555", "Portal", []string{"office"}); err != nil {
		t.Fatalf("CreateWireGuard on migrated DB: %v", err)
	}
}

// TestInitManagerMigrationIsIdempotent runs InitManager twice on the same DB;
// the second run must not error on the already-present columns.
func TestInitManagerMigrationIsIdempotent(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "twice.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	if _, err := InitManager(db); err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	if _, err := InitManager(db); err != nil {
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
	if err := validateNetworkScope(nil); !errors.Is(err, ErrWireGuardNoNetworks) {
		t.Errorf("empty: got %v, want ErrWireGuardNoNetworks", err)
	}
	if err := validateNetworkScope([]string{"ok", "BAD NAME"}); !errors.Is(err, ErrInvalidNetworkName) {
		t.Errorf("bad name: got %v, want ErrInvalidNetworkName", err)
	}
	if err := validateNetworkScope([]string{"office", "home-lab"}); err != nil {
		t.Errorf("valid scope: got %v, want nil", err)
	}
}
