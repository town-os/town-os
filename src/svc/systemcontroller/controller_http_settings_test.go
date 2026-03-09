// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

func TestHTTPSettingsDefaultsOnInit(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Defaults should be present without any explicit Set calls.
	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting default_quota: %v", err)
	}
	if val != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected default %q, got %q", account.DefaultSettings["default_quota"], val)
	}

	// List should include all defaults.
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

func TestHTTPSettingsSetAndGet(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Override the seeded default.
	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "0" {
		t.Fatalf("expected %q, got %q", "0", val)
	}
}

func TestHTTPSettingsGetNotFound(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	_, err := c.GetSetting(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent setting")
	}
}

func TestHTTPSettingsList(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Add a custom setting alongside the seeded defaults.
	if err := c.SetSetting(context.TODO(), "custom_key", "hello"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	// Should have all defaults plus the custom key.
	wantLen := len(account.DefaultSettings) + 1
	if len(settings) != wantLen {
		t.Fatalf("expected %d settings, got %d: %v", wantLen, len(settings), settings)
	}
	if settings["default_quota"] != account.DefaultSettings["default_quota"] {
		t.Fatalf("expected default_quota %q, got %q", account.DefaultSettings["default_quota"], settings["default_quota"])
	}
	if settings["custom_key"] != "hello" {
		t.Fatalf("expected custom_key %q, got %q", "hello", settings["custom_key"])
	}
}

func TestHTTPSettingsOverwrite(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "100"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := c.SetSetting(context.TODO(), "default_quota", "200"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "default_quota")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "200" {
		t.Fatalf("expected %q, got %q", "200", val)
	}
}

func TestHTTPSettingsSetNewKey(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "motd", "welcome"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "motd")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "welcome" {
		t.Fatalf("expected %q, got %q", "welcome", val)
	}
}

func TestHTTPSettingsSetAuditLog(t *testing.T) {
	c, token := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "default_quota", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{}, token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	found := false
	for _, e := range page.Entries {
		if e.Action == "update setting" && e.Path == "/settings/set" {
			found = true
			if !e.Success {
				t.Fatal("expected success to be true")
			}
			if e.Account != "testadmin" {
				t.Fatalf("expected account %q, got %q", "testadmin", e.Account)
			}
			if e.Detail == "" {
				t.Fatal("expected non-empty audit detail")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'update setting' audit entry")
	}
}

func TestHTTPSettingsGetNotAudited(t *testing.T) {
	c, token := initSettingsTestClient(t)

	// Read operations should not appear in audit log.
	if _, err := c.GetSetting(context.TODO(), "default_quota"); err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if _, err := c.GetSettings(context.TODO()); err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	page, err := c.ListAuditLog(context.TODO(), account.AuditListOptions{}, token)
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}

	for _, e := range page.Entries {
		if e.Path == "/settings/get" || e.Path == "/settings" {
			t.Fatalf("read-only settings path %q should not be audited", e.Path)
		}
	}
}

func TestHTTPSettingsRequiresAdmin(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Create a non-admin user.
	if _, err := c.CreateAccount(context.TODO(), "user", "password1", "u@b.com", "555", "User", false); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "user", "password1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
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

// --- Settings byte-value normalization tests ---

func TestHTTPSettingsQuotaHumanReadable(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	table := []struct {
		input    string
		expected string
	}{
		{"500GB", "536870912000"},
		{"500gb", "536870912000"},
		{"1TB", "1099511627776"},
		{"100MB", "104857600"},
		{"0", "0"},
		{"1073741824", "1073741824"},
	}

	for _, tc := range table {
		if err := c.SetSetting(context.TODO(), "default_quota", tc.input); err != nil {
			t.Fatalf("SetSetting(%q): %v", tc.input, err)
		}
		val, err := c.GetSetting(context.TODO(), "default_quota")
		if err != nil {
			t.Fatalf("GetSetting after setting %q: %v", tc.input, err)
		}
		if val != tc.expected {
			t.Fatalf("SetSetting(%q): expected stored value %q, got %q", tc.input, tc.expected, val)
		}
	}
}

func TestHTTPSettingsQuotaInvalidValue(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	badValues := []string{"not-a-number", "-5GB", "abc"}
	for _, v := range badValues {
		if err := c.SetSetting(context.TODO(), "default_quota", v); err == nil {
			t.Fatalf("expected error for invalid quota value %q", v)
		}
	}
}

func TestHTTPSettingsNonQuotaKeyNotNormalized(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Non-quota keys should store values as-is without byte parsing.
	if err := c.SetSetting(context.TODO(), "motd", "500GB"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	val, err := c.GetSetting(context.TODO(), "motd")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "500GB" {
		t.Fatalf("expected non-quota key to store raw value %q, got %q", "500GB", val)
	}
}

func TestHTTPSettingsIncludesArchiveDefaults(t *testing.T) {
	c, _ := initSettingsTestClient(t)
	settings, err := c.GetSettings(context.TODO())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	if _, ok := settings["max_archive_size"]; !ok {
		t.Fatal("expected max_archive_size in settings")
	}
	if _, ok := settings["archive_unpack_timeout"]; !ok {
		t.Fatal("expected archive_unpack_timeout in settings")
	}
}

func TestHTTPSettingsMaxArchiveSizeHumanReadable(t *testing.T) {
	c, _ := initSettingsTestClient(t)
	if err := c.SetSetting(context.TODO(), "max_archive_size", "100MB"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "max_archive_size")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "104857600" {
		t.Fatalf("expected 104857600, got %q", val)
	}
}

func TestHTTPSettingsMaxArchiveSizeSetAndGet(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// Set with raw numeric bytes.
	if err := c.SetSetting(context.TODO(), "max_archive_size", "52428800"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "max_archive_size")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "52428800" {
		t.Fatalf("expected %q, got %q", "52428800", val)
	}
}

func TestHTTPSettingsMaxArchiveSizeInvalidValue(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	badValues := []string{"not-a-number", "-5MB", "abc"}
	for _, v := range badValues {
		if err := c.SetSetting(context.TODO(), "max_archive_size", v); err == nil {
			t.Fatalf("expected error for invalid max_archive_size value %q", v)
		}
	}
}

func TestHTTPSettingsArchiveUnpackTimeoutSetAndGet(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	// archive_unpack_timeout is not a byte-value setting, so it stores as-is.
	if err := c.SetSetting(context.TODO(), "archive_unpack_timeout", "300"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "archive_unpack_timeout")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "300" {
		t.Fatalf("expected %q, got %q", "300", val)
	}
}

func TestHTTPSettingsArchiveUnpackTimeoutOverwrite(t *testing.T) {
	c, _ := initSettingsTestClient(t)

	if err := c.SetSetting(context.TODO(), "archive_unpack_timeout", "60"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := c.SetSetting(context.TODO(), "archive_unpack_timeout", "600"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}

	val, err := c.GetSetting(context.TODO(), "archive_unpack_timeout")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "600" {
		t.Fatalf("expected %q, got %q", "600", val)
	}
}
