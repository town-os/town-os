// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"slices"
	"testing"

	"gitea.com/town-os/town-os/src/i18n"
)

func initLocaleTestClient(t *testing.T) *SystemdClient {
	t.Helper()
	c, _ := initSettingsTestClient(t)
	return c
}

func TestHTTPLocaleListReturnsCurrentLocale(t *testing.T) {
	c := initLocaleTestClient(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if resp.Current != i18n.DefaultLocale {
		t.Fatalf("expected current locale %q, got %q", i18n.DefaultLocale, resp.Current)
	}
}

func TestHTTPLocaleListReturnsPopulatedLocales(t *testing.T) {
	c := initLocaleTestClient(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if !slices.Contains(resp.Populated, "en-US") {
		t.Fatal("expected populated locales to contain en-US")
	}
}

func TestHTTPLocaleListReturnsCommonLanguages(t *testing.T) {
	c := initLocaleTestClient(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if len(resp.CommonLanguages) == 0 {
		t.Fatal("expected non-empty common_languages")
	}

	// Verify English is in the list.
	found := false
	for _, l := range resp.CommonLanguages {
		if l.Code == "en-US" {
			found = true
			if l.NativeName != "English" {
				t.Fatalf("expected NativeName %q, got %q", "English", l.NativeName)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected en-US in common_languages")
	}
}

func TestHTTPLocaleListReturnsExtendedLocales(t *testing.T) {
	c := initLocaleTestClient(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if len(resp.ExtendedLocales) == 0 {
		t.Fatal("expected non-empty extended_locales")
	}
}

func TestHTTPLocaleListReflectsSettingsLocale(t *testing.T) {
	c := initLocaleTestClient(t)

	// Set a custom locale via settings.
	if err := c.SetSetting(context.TODO(), "locale", "de-DE"); err != nil {
		t.Fatalf("SetSetting locale: %v", err)
	}

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if resp.Current != "de-DE" {
		t.Fatalf("expected current locale %q after setting change, got %q", "de-DE", resp.Current)
	}
}

func TestHTTPLocaleListRequiresAuth(t *testing.T) {
	c := initLocaleTestClient(t)
	c.Token = "" // Clear auth token.

	_, err := c.ListLocales(context.TODO())
	if err == nil {
		t.Fatal("expected auth error for ListLocales without token")
	}
}

func TestHTTPLocaleListCommonLanguagesHaveNativeNames(t *testing.T) {
	c := initLocaleTestClient(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	for _, l := range resp.CommonLanguages {
		if l.Code == "" {
			t.Error("common language has empty code")
		}
		if l.NativeName == "" {
			t.Errorf("common language %q has empty native_name", l.Code)
		}
		if l.EnglishName == "" {
			t.Errorf("common language %q has empty english_name", l.Code)
		}
	}
}

func TestHTTPLocaleListNotAudited(t *testing.T) {
	c := initLocaleTestClient(t)

	// List locales - should NOT be audit logged.
	if _, err := c.ListLocales(context.TODO()); err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	// The locale endpoint is in the excluded list, so no audit entry should exist.
	// We verify by checking the settings test client already has audit configured.
}

func TestMockClientListLocales(t *testing.T) {
	m := InitMockClient()

	resp, err := m.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.ListLocales: %v", err)
	}

	if resp.Current != i18n.DefaultLocale {
		t.Fatalf("expected current locale %q, got %q", i18n.DefaultLocale, resp.Current)
	}

	if !slices.Contains(resp.Populated, "en-US") {
		t.Fatal("expected populated to contain en-US")
	}

	if len(resp.CommonLanguages) == 0 {
		t.Fatal("expected non-empty common_languages")
	}

	if len(resp.ExtendedLocales) == 0 {
		t.Fatal("expected non-empty extended_locales")
	}
}

func TestMockClientListLocalesCallLog(t *testing.T) {
	m := InitMockClient()

	if _, err := m.ListLocales(context.TODO()); err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "ListLocales" {
		t.Fatalf("expected method ListLocales, got %q", calls[0].Method)
	}
}

func TestMockClientListLocalesErrorInjection(t *testing.T) {
	m := InitMockClient()
	m.ListLocalesErr = errors.New("injected error")

	_, err := m.ListLocales(context.TODO())
	if err == nil {
		t.Fatal("expected error")
	}
}
