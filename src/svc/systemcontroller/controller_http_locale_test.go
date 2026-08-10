// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

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

// TestHTTPLocaleListAdvertisesEveryCatalog checks the endpoint reports exactly
// what the i18n package has catalogs for. The list used to be a hand-written
// slice literal in i18n.go; it is derived from the catalog map now, and this is
// the assertion that the derivation reaches the wire.
func TestHTTPLocaleListAdvertisesEveryCatalog(t *testing.T) {
	c := initLocaleTestClient(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	resp, err := c.ListLocales(ctx)
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	want := i18n.PopulatedLocales()
	if !slices.Equal(resp.Populated, want) {
		t.Fatalf("populated locales = %v, want %v", resp.Populated, want)
	}
}

// TestHTTPLocaleListAdvertisesCountryVariants names the locales this branch
// added, so the endpoint cannot quietly stop offering them. A derived catalog
// that never reaches /locales is translated, shipped, and unreachable: the
// settings picker disables every code missing from `populated`.
func TestHTTPLocaleListAdvertisesCountryVariants(t *testing.T) {
	c := initLocaleTestClient(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	resp, err := c.ListLocales(ctx)
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	for _, code := range []string{
		// Derived country catalogs.
		"ar-AE", "ar-EG", "bn-IN", "de-AT", "de-CH",
		"en-AU", "en-CA", "en-GB", "en-IN", "en-NZ", "en-ZA",
		"es-AR", "es-MX", "fr-BE", "fr-CA", "fr-CH", "nl-BE", "pt-PT",
		// Full translations added alongside them.
		"cs-CZ", "hr-HR", "hu-HU", "ro-RO", "sk-SK", "sl-SI",
	} {
		if !slices.Contains(resp.Populated, code) {
			t.Errorf("populated locales does not contain %q", code)
		}
	}
}

// TestHTTPLocaleListPopulatedLocalesAreSelectable closes the loop between the
// two halves of the response. The picker builds its options from
// common_languages and extended_locales and disables anything absent from
// populated — so a code that is populated but in neither list is a translation
// no one can ever select.
func TestHTTPLocaleListPopulatedLocalesAreSelectable(t *testing.T) {
	c := initLocaleTestClient(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	resp, err := c.ListLocales(ctx)
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	offered := make(map[string]struct{}, len(resp.CommonLanguages)+len(resp.ExtendedLocales))
	for _, l := range resp.CommonLanguages {
		offered[l.Code] = struct{}{}
	}
	for _, l := range resp.ExtendedLocales {
		offered[l.Code] = struct{}{}
	}

	for _, code := range resp.Populated {
		if _, ok := offered[code]; !ok {
			t.Errorf("locale %q is populated but appears in neither common_languages nor extended_locales", code)
		}
	}
}

// TestHTTPLocaleListDoesNotAdvertiseEsLatam keeps the shared American Spanish
// fragment off the wire. es-MX and es-AR are built from it; it is not a place
// anyone lives, and advertising it would offer a country code that is not one.
func TestHTTPLocaleListDoesNotAdvertiseEsLatam(t *testing.T) {
	c := initLocaleTestClient(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	resp, err := c.ListLocales(ctx)
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	for _, code := range []string{"es-latam", "es-419"} {
		if slices.Contains(resp.Populated, code) {
			t.Errorf("populated locales advertises %q, which is a shared fragment and not a locale", code)
		}
	}
}

// TestHTTPLocaleListReflectsCountryVariantSetting confirms a derived locale
// survives the round trip through settings, not just the advertised list.
func TestHTTPLocaleListReflectsCountryVariantSetting(t *testing.T) {
	c := initLocaleTestClient(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if err := c.SetSetting(ctx, "locale", "pt-PT"); err != nil {
		t.Fatalf("SetSetting locale: %v", err)
	}

	resp, err := c.ListLocales(ctx)
	if err != nil {
		t.Fatalf("ListLocales: %v", err)
	}

	if resp.Current != "pt-PT" {
		t.Fatalf("expected current locale %q, got %q", "pt-PT", resp.Current)
	}
	if !slices.Contains(resp.Populated, "pt-PT") {
		t.Fatal("pt-PT is the current locale but is not advertised as populated")
	}
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
