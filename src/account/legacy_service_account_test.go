// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"errors"
	"path/filepath"
	"testing"
)

// initPurgeTestDB returns a manager plus the handle the purge needs, since the
// purge is deliberately below the Manager interface.
func initPurgeTestDB(t *testing.T) (*SQLiteManager, *SQLiteSettingsManager) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "purge.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	mgr, err := InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	settings, err := InitSettingsManager(db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}
	t.Cleanup(func() {
		if _, err := PurgeLegacyServiceAccounts(db); err != nil {
			t.Errorf("cleanup purge: %v", err)
		}
	})
	return mgr, settings
}

// An upgraded box carries the daemon account from the release that created it.
// Nothing else would ever remove it, so it would keep showing up in the users
// list and the admin count as an account the operator never made.
func TestPurgeRemovesTheLegacyServiceAccount(t *testing.T) {
	mgr, settings := initPurgeTestDB(t)

	if _, err := mgr.Create(LegacyGfehServiceAccount, "purgeme12345", "gfeh@service.town-os.local", "0000000000", "Object Storage Service", true); err != nil {
		t.Fatalf("seed legacy account: %v", err)
	}
	if err := settings.Set(LegacyGfehPasswordSetting, "deadbeef"); err != nil {
		t.Fatalf("seed legacy password: %v", err)
	}

	removed, err := PurgeLegacyServiceAccounts(mgr.db)
	if err != nil {
		t.Fatalf("PurgeLegacyServiceAccounts: %v", err)
	}
	if !removed {
		t.Error("purge reported nothing removed, but the account was there")
	}

	if _, err := mgr.Get(LegacyGfehServiceAccount); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after purge = %v, want ErrNotFound", err)
	}

	// The password goes with it: left behind it is an administrator credential
	// for an account that no longer exists.
	if v, err := settings.Get(LegacyGfehPasswordSetting); err == nil && v != "" {
		t.Errorf("the stored service password survived the purge (%q)", v)
	}
}

// Idempotent, because it runs on every boot: the second call must be a quiet
// no-op rather than an error, and must report that it removed nothing so the
// caller does not log a removal that did not happen.
func TestPurgeIsIdempotent(t *testing.T) {
	mgr, _ := initPurgeTestDB(t)

	if _, err := mgr.Create(LegacyGfehServiceAccount, "purgeme12345", "gfeh@service.town-os.local", "0000000000", "Object Storage Service", true); err != nil {
		t.Fatalf("seed legacy account: %v", err)
	}
	if _, err := PurgeLegacyServiceAccounts(mgr.db); err != nil {
		t.Fatalf("first purge: %v", err)
	}

	removed, err := PurgeLegacyServiceAccounts(mgr.db)
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if removed {
		t.Error("the second purge claimed to remove something")
	}
}

// It must take the daemon account and nothing else. A purge that reached any
// further would be deleting accounts on boot with no way for the operator to
// stop it.
func TestPurgeLeavesEveryOtherAccountAlone(t *testing.T) {
	mgr, _ := initPurgeTestDB(t)

	if _, err := mgr.Create("townos", "adminpass123", "a@example.com", "555-1000", "Town OS", true); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if _, err := mgr.Create(LegacyGfehServiceAccount, "purgeme12345", "gfeh@service.town-os.local", "0000000000", "Object Storage Service", true); err != nil {
		t.Fatalf("seed legacy account: %v", err)
	}

	if _, err := PurgeLegacyServiceAccounts(mgr.db); err != nil {
		t.Fatalf("purge: %v", err)
	}

	accounts, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(accounts) != 1 || accounts[0].Username != "townos" {
		t.Errorf("accounts after purge = %+v, want only townos", accounts)
	}
}

// A database that never had the account -- a fresh box -- must not error.
func TestPurgeOnAFreshDatabase(t *testing.T) {
	mgr, _ := initPurgeTestDB(t)

	removed, err := PurgeLegacyServiceAccounts(mgr.db)
	if err != nil {
		t.Fatalf("purge on a fresh database: %v", err)
	}
	if removed {
		t.Error("purge claimed to remove an account from a database that never had one")
	}
}
