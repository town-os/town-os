// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// --- Settings integration tests (admin and user) ---

func TestSettingsDefaultsOnInit(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Defaults should be present without any explicit Set calls.
	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting default_quota: %v", err)
	}
	if val != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected default %q, got %q", account.DefaultSettings["default_quota"], val)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	for k, want := range account.DefaultSettings {
		got, ok := settings[k]
		if !ok {
			t.Fatalf("expected default key %q in list", k)
		}
		if got != want {
			t.Fatalf("default %q: expected %q, got %q", k, want, got)
		}
	}
}

func TestSettingsAdminCanSetAndGet(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "1073741824"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "1073741824" {
		t.Fatalf("expected %q, got %q", "1073741824", val)
	}
}

func TestSettingsAdminCanSetHumanReadable(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "500GB"); err != nil {
		t.Fatalf("SetSetting 500GB: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "536870912000" {
		t.Fatalf("expected %q, got %q", "536870912000", val)
	}
}

func TestSettingsNonAdminRejected(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Create a non-admin user and switch to their token.
	if _, err := c.CreateAccount(context.TODO(), "user", "userpass", "user@test.com", "555-1111", "User", false); err != nil {
		t.Fatalf("CreateAccount user: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "user", "userpass")
	if err != nil {
		t.Fatalf("Authenticate user: %v", err)
	}
	c.Token = resp.Token

	// All settings endpoints should reject non-admin.
	if _, err := c.GetSettings(context.TODO()); err == nil {
		t.Fatal("expected error for non-admin GetSettings")
	}
	if _, err := c.GetSetting(context.TODO(), "default_quota"); err == nil {
		t.Fatal("expected error for non-admin GetSetting")
	}
	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err == nil {
		t.Fatal("expected error for non-admin SetSetting")
	}
}

func TestSettingsAdminOverrideAndList(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Set a custom key.
	if err := c.SetSetting(context.TODO(), "motd", "hello world"); err != nil {
		t.Fatalf("SetSetting motd: %v", err)
	}

	// Override the default quota.
	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err != nil {
		t.Fatalf("SetSetting default_quota: %v", err)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	if settings["motd"] != "hello world" {
		t.Fatalf("expected motd %q, got %q", "hello world", settings["motd"])
	}
	if settings["default_quota"] != "0" {
		t.Fatalf("expected default_quota %q, got %q", "0", settings["default_quota"])
	}
}

func TestSettingsMaxArchiveSizeSetAndGet(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Set max archive size using human-readable value.
	if err := c.SetSetting(context.TODO(), "max_archive_size", "100MB"); err != nil {
		t.Fatalf("SetSetting max_archive_size: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "max_archive_size")
	if err != nil {
		t.Fatalf("GetSetting max_archive_size: %v", err)
	}
	if val != "104857600" {
		t.Fatalf("expected %q, got %q", "104857600", val)
	}
}

func TestSettingsMaxArchiveSizeNumericValue(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Set max archive size using raw numeric bytes.
	if err := c.SetSetting(context.TODO(), "max_archive_size", "52428800"); err != nil {
		t.Fatalf("SetSetting max_archive_size: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "max_archive_size")
	if err != nil {
		t.Fatalf("GetSetting max_archive_size: %v", err)
	}
	if val != "52428800" {
		t.Fatalf("expected %q, got %q", "52428800", val)
	}
}

func TestSettingsArchiveUnpackTimeoutSetAndGet(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Set archive unpack timeout (stored as-is, not byte-parsed).
	if err := c.SetSetting(context.TODO(), "archive_unpack_timeout", "300"); err != nil {
		t.Fatalf("SetSetting archive_unpack_timeout: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "archive_unpack_timeout")
	if err != nil {
		t.Fatalf("GetSetting archive_unpack_timeout: %v", err)
	}
	if val != "300" {
		t.Fatalf("expected %q, got %q", "300", val)
	}
}

func TestSettingsArchiveUnpackTimeoutInList(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	if err := c.SetSetting(context.TODO(), "archive_unpack_timeout", "600"); err != nil {
		t.Fatalf("SetSetting archive_unpack_timeout: %v", err)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	if settings["archive_unpack_timeout"] != "600" {
		t.Fatalf("expected archive_unpack_timeout %q, got %q", "600", settings["archive_unpack_timeout"])
	}
}
