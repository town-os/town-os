// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"slices"
	"testing"

	"gitea.com/town-os/town-os/src/i18n"
)

// --- Locale integration tests ---

func TestLocalesDefaultResponse(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if resp.Current != i18n.DefaultLocale {
		t.Fatalf("expected current %q, got %q", i18n.DefaultLocale, resp.Current)
	}
}

func TestLocalesPopulatedContainsEnUS(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if !slices.Contains(resp.Populated, "en-US") {
		t.Fatal("expected populated to contain en-US")
	}
}

func TestLocalesCommonLanguagesNotEmpty(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if len(resp.CommonLanguages) == 0 {
		t.Fatal("expected common_languages to be non-empty")
	}
}

func TestLocalesExtendedLocalesNotEmpty(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if len(resp.ExtendedLocales) == 0 {
		t.Fatal("expected extended_locales to be non-empty")
	}
}

func TestLocalesReflectsLocaleSetting(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Change the locale setting.
	if err := c.SetSetting(context.TODO(), "locale", "de-DE"); err != nil {
		t.Fatalf("SetSetting locale: %v", err)
	}

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if resp.Current != "de-DE" {
		t.Fatalf("expected current %q after setting change, got %q", "de-DE", resp.Current)
	}
}

func TestLocalesCommonLanguagesHaveEnglish(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	found := false
	for _, lang := range resp.CommonLanguages {
		if lang.Code == "en-US" && lang.NativeName == "English" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected common_languages to contain English (en-US)")
	}
}

func TestLocalesCommonLanguagesHaveRequiredFields(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	resp, err := c.ListLocales(context.TODO())
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	for _, lang := range resp.CommonLanguages {
		if lang.Code == "" {
			t.Error("common language entry has empty Code")
		}
		if lang.NativeName == "" {
			t.Errorf("common language %q has empty NativeName", lang.Code)
		}
		if lang.EnglishName == "" {
			t.Errorf("common language %q has empty EnglishName", lang.Code)
		}
	}
}

func TestLocalesRequiresAuth(t *testing.T) {
	c := initSystemControllerSettingsTest(t)

	// Clear the token to simulate unauthenticated request.
	c.Token = ""

	_, err := c.ListLocales(context.TODO())
	if err == nil {
		t.Fatal("expected error for unauthenticated ListLocales")
	}
}
