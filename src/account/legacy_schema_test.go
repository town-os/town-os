// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// legacySchema is the accounts table exactly as the previous release wrote it:
// wireguard, object_storage, and smb_nt_hash all present, network_only absent.
//
// Spelled out here rather than derived from the current schema, because the
// point of these tests is that a table this code no longer produces is still
// migrated correctly. A helper that built it from today's definition would stop
// testing anything the day the definition changed.
const legacySchema = `CREATE TABLE accounts (
	username       TEXT PRIMARY KEY,
	password_hash  TEXT NOT NULL,
	email          TEXT NOT NULL,
	phone          TEXT NOT NULL,
	real_name      TEXT NOT NULL,
	admin          INTEGER NOT NULL DEFAULT 0,
	disabled       INTEGER NOT NULL DEFAULT 0,
	wireguard      INTEGER NOT NULL DEFAULT 0,
	networks       TEXT NOT NULL DEFAULT '[]',
	object_storage INTEGER NOT NULL DEFAULT 0,
	smb_nt_hash    TEXT NOT NULL DEFAULT '',
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL
)`

// legacyRow inserts one account through the legacy schema.
const legacyRow = `INSERT INTO accounts
	(username, password_hash, email, phone, real_name, admin, disabled, wireguard, networks, object_storage, smb_nt_hash, created_at, updated_at)
	VALUES (?, 'notarealhash', ?, '555-1234', ?, ?, 0, ?, ?, ?, ?, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`

// openLegacyDB builds a database at the legacy schema, populated with three
// rows that between them cover every combination the old model could hold: an
// administrator, a WireGuard account with a scope, and a plain account holding
// the standalone object-storage capability and an enrolled SMB hash.
func openLegacyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), dbTimeout)
	defer cancel()

	if _, err := db.ExecContext(ctx, legacySchema); err != nil {
		t.Fatalf("create legacy accounts table: %v", err)
	}
	for _, row := range []struct {
		username, email, realName string
		admin, wireguard          int
		networks, objectStorage   any
		ntHash                    string
	}{
		{"root", "root@example.com", "Root", 1, 0, "[]", 0, ""},
		{"portal", "portal@example.com", "Portal", 0, 1, `["office"]`, 0, "a4f49c406510bdcab6824ee7c30fd852"},
		{"bob", "bob@example.com", "Bob", 0, 0, "[]", 1, "b4f49c406510bdcab6824ee7c30fd852"},
	} {
		if _, err := db.ExecContext(ctx, legacyRow,
			row.username, row.email, row.realName, row.admin, row.wireguard, row.networks, row.objectStorage, row.ntHash,
		); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.username, err)
		}
	}
	return db
}

// The WireGuard column becomes the wireguard grant, with its scope intact.
// Losing it would silently promote a restricted account to a full dashboard
// account on the first boot after an upgrade — the account keeps its password,
// and nothing on screen would say its confinement had been lifted.
func TestMigrationCarriesWireGuardIntoNetworkOnly(t *testing.T) {
	mgr, err := InitManager(t.Context(), openLegacyDB(t))
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	got, err := mgr.Get(t.Context(), "portal")
	if err != nil {
		t.Fatalf("Get portal: %v", err)
	}
	if !got.HasGrant(GrantWireGuard) {
		t.Errorf("a WireGuard account did not come back holding %q: %v", GrantWireGuard, got.Grants)
	}
	if len(got.Networks) != 1 || got.Networks[0] != "office" {
		t.Errorf("scope = %v, want [office]", got.Networks)
	}
	if !got.MayAdministerNetwork("office") {
		t.Error("the migrated account cannot administer the network it is scoped to")
	}
	if got.MayAdministerNetwork("home") {
		t.Error("the migrated account escaped its scope")
	}
}

// Each legacy column becomes its own grant and no other. The account that held
// object_storage gets the gfeh grant and NOT wireguard: silently handing it peer
// enrollment during an upgrade is the direction you cannot take back.
func TestMigrationDoesNotInventNetworkOnlyAccounts(t *testing.T) {
	mgr, err := InitManager(t.Context(), openLegacyDB(t))
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	for _, username := range []string{"root", "bob"} {
		got, err := mgr.Get(t.Context(), username)
		if err != nil {
			t.Fatalf("Get %s: %v", username, err)
		}
		if got.HasGrant(GrantWireGuard) && !got.Admin {
			t.Errorf("%s gained the wireguard grant across the migration: %v", username, got.Grants)
		}
	}

	bob, err := mgr.Get(t.Context(), "bob")
	if err != nil {
		t.Fatalf("Get bob: %v", err)
	}
	if !bob.HasGrant(GrantGfeh) {
		t.Errorf("the object_storage column did not become the gfeh grant: %v", bob.Grants)
	}

	root, err := mgr.Get(t.Context(), "root")
	if err != nil {
		t.Fatalf("Get root: %v", err)
	}
	if !root.Admin {
		t.Error("the administrator lost its admin flag across the migration")
	}
	if !root.HoldsEveryGrant() {
		t.Error("the migrated administrator does not hold every grant")
	}
}

// The legacy columns go. smb_nt_hash matters most: an NT hash is unsalted MD4
// with no work factor and password-equivalent to anything still speaking NTLM,
// so it is the weakest secret at rest in the database — and it now buys
// nothing, since no account carries an SMB password and no partition serves the
// view. Nothing else would ever remove it from an upgraded box.
func TestMigrationDropsLegacyColumns(t *testing.T) {
	db := openLegacyDB(t)
	if _, err := InitManager(t.Context(), db); err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), dbTimeout)
	defer cancel()

	for _, col := range []string{"smb_nt_hash", "object_storage", "wireguard", "network_only"} {
		present, err := columnExists(ctx, db, col)
		if err != nil {
			t.Fatalf("columnExists(%q): %v", col, err)
		}
		if present {
			t.Errorf("accounts.%s survived the migration", col)
		}
	}

	present, err := columnExists(ctx, db, "grants")
	if err != nil {
		t.Fatalf("columnExists(grants): %v", err)
	}
	if !present {
		t.Error("accounts.grants was not added")
	}
}

// Running InitManager repeatedly is the normal case — every boot after the
// first — and later runs find nothing to migrate. The copy is written to be
// idempotent and the drops are guarded by a column lookup rather than by
// matching SQLite's error text, so both must survive being re-run.
func TestMigrationIsIdempotentAcrossBoots(t *testing.T) {
	db := openLegacyDB(t)

	for i := range 3 {
		mgr, err := InitManager(t.Context(), db)
		if err != nil {
			t.Fatalf("InitManager run %d: %v", i+1, err)
		}
		got, err := mgr.Get(t.Context(), "portal")
		if err != nil {
			t.Fatalf("Get portal after run %d: %v", i+1, err)
		}
		if !got.HasGrant(GrantWireGuard) {
			t.Fatalf("run %d lost the wireguard grant: %v", i+1, got.Grants)
		}
	}
}

// A database that never had any of these columns takes the same path on every
// boot and must not error there.
func TestMigrationOnAFreshDatabase(t *testing.T) {
	db, err := OpenDB(t.Context(), filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	if _, err := InitManager(t.Context(), db); err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), dbTimeout)
	defer cancel()

	for _, col := range []string{"smb_nt_hash", "object_storage", "wireguard", "network_only"} {
		present, err := columnExists(ctx, db, col)
		if err != nil {
			t.Fatalf("columnExists(%q): %v", col, err)
		}
		if present {
			t.Errorf("a fresh database created accounts.%s", col)
		}
	}
}

// columnExists is load-bearing for every drop above, so it is pinned on both
// answers rather than only on the one the migration happens to take.
func TestColumnExists(t *testing.T) {
	db := openLegacyDB(t)

	ctx, cancel := context.WithTimeout(t.Context(), dbTimeout)
	defer cancel()

	for _, tc := range []struct {
		column string
		want   bool
	}{
		{"username", true},
		{"smb_nt_hash", true},
		{"wireguard", true},
		{"grants", false},
		{"nonexistent", false},
	} {
		got, err := columnExists(ctx, db, tc.column)
		if err != nil {
			t.Fatalf("columnExists(%q): %v", tc.column, err)
		}
		if got != tc.want {
			t.Errorf("columnExists(%q) = %v, want %v", tc.column, got, tc.want)
		}
	}
}
