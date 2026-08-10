// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"path/filepath"
	"testing"
)

func initTestSettingsDB(t *testing.T) *SQLiteSettingsManager {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}
	return mgr
}

func TestSettingsDefaultsSeeded(t *testing.T) {
	mgr := initTestSettingsDB(t)

	for key, expected := range DefaultSettings {
		val, err := mgr.Get(t.Context(), key)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		if val != expected {
			t.Fatalf("expected %q=%q, got %q", key, expected, val)
		}
	}
}

func TestSettingsSetAndGet(t *testing.T) {
	mgr := initTestSettingsDB(t)

	if err := mgr.Set(t.Context(), "custom_key", "custom_value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := mgr.Get(t.Context(), "custom_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "custom_value" {
		t.Fatalf("expected custom_value, got %s", val)
	}
}

func TestSettingsSetOverwrites(t *testing.T) {
	mgr := initTestSettingsDB(t)

	if err := mgr.Set(t.Context(), "key", "first"); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := mgr.Set(t.Context(), "key", "second"); err != nil {
		t.Fatalf("Set second: %v", err)
	}

	val, err := mgr.Get(t.Context(), "key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "second" {
		t.Fatalf("expected second, got %s", val)
	}
}

func TestSettingsGetNotFound(t *testing.T) {
	mgr := initTestSettingsDB(t)

	_, err := mgr.Get(t.Context(), "nonexistent_key")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestSettingsList(t *testing.T) {
	mgr := initTestSettingsDB(t)

	if err := mgr.Set(t.Context(), "extra_key", "extra_value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	all, err := mgr.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(all) < len(DefaultSettings)+1 {
		t.Fatalf("expected at least %d settings, got %d", len(DefaultSettings)+1, len(all))
	}

	if all["extra_key"] != "extra_value" {
		t.Fatalf("expected extra_key=extra_value, got %s", all["extra_key"])
	}
}

func TestSettingsDefaultsNotOverwritten(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	mgr, err := InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("first InitSettingsManager: %v", err)
	}

	if err := mgr.Set(t.Context(), "default_quota", "999"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Re-init should not overwrite existing values.
	mgr2, err := InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("second InitSettingsManager: %v", err)
	}

	val, err := mgr2.Get(t.Context(), "default_quota")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "999" {
		t.Fatalf("expected custom value 999 preserved, got %s", val)
	}
}

func TestSettingsImplementsInterface(t *testing.T) {
	var _ SettingsManager = (*SQLiteSettingsManager)(nil)
}
