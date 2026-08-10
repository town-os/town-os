// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package i18n

import (
	"maps"
	"testing"
)

// variant names one country catalog and the pieces it was built from, so the
// tests below can hold every one of them to the same rules without repeating
// themselves once per locale.
type variant struct {
	code      string
	base      map[string]string
	overrides map[string]string
	messages  map[string]string
}

// variants lists every country catalog built by derive(). A new one added to
// the package but not to this list is invisible to these tests, which is what
// TestEveryDerivedCatalogIsUnderTest exists to catch.
func variants() []variant {
	return []variant{
		{"en-GB", enUSMessages, enGBOverrides, enGBMessages},
		{"en-AU", enGBMessages, enAUOverrides, enAUMessages},
		{"en-IN", enGBMessages, enINOverrides, enINMessages},
		{"en-NZ", enGBMessages, enNZOverrides, enNZMessages},
		{"en-ZA", enGBMessages, enZAOverrides, enZAMessages},
		{"en-CA", enUSMessages, enCAOverrides, enCAMessages},
		{"de-AT", deDEMessages, deATOverrides, deATMessages},
		{"de-CH", deDEMessages, deCHOverrides, deCHMessages},
		{"fr-BE", frFRMessages, frBEOverrides, frBEMessages},
		{"fr-CA", frFRMessages, frCAOverrides, frCAMessages},
		{"fr-CH", frFRMessages, frCHOverrides, frCHMessages},
		{"es-MX", esLatamMessages, esMXOverrides, esMXMessages},
		{"es-AR", esLatamMessages, esAROverrides, esARMessages},
		{"pt-PT", ptBRMessages, ptPTOverrides, ptPTMessages},
		{"nl-BE", nlNLMessages, nlBEOverrides, nlBEMessages},
		{"ar-AE", arSAMessages, arAEOverrides, arAEMessages},
		{"ar-EG", arSAMessages, arEGOverrides, arEGMessages},
		{"bn-IN", bnBDMessages, bnINOverrides, bnINMessages},
	}
}

func TestDeriveAppliesOverridesOverBase(t *testing.T) {
	base := map[string]string{"a": "base-a", "b": "base-b"}
	got := derive(base, map[string]string{"b": "over-b"})

	if got["a"] != "base-a" {
		t.Errorf("derive dropped an inherited key: got %q, want %q", got["a"], "base-a")
	}
	if got["b"] != "over-b" {
		t.Errorf("derive did not apply the override: got %q, want %q", got["b"], "over-b")
	}
}

// TestDeriveDoesNotMutateBase is the property the whole scheme rests on: every
// variant of a language derives from the same base map, so a derive() that
// wrote through to its argument would let the last country registered rewrite
// the language for everyone.
func TestDeriveDoesNotMutateBase(t *testing.T) {
	base := map[string]string{"a": "base-a"}
	overrides := map[string]string{"a": "over-a"}

	derive(base, overrides)

	if base["a"] != "base-a" {
		t.Errorf("derive mutated its base: got %q, want %q", base["a"], "base-a")
	}
	if overrides["a"] != "over-a" {
		t.Errorf("derive mutated its overrides: got %q, want %q", overrides["a"], "over-a")
	}
}

func TestDeriveWithNoOverridesCopiesBase(t *testing.T) {
	base := map[string]string{"a": "base-a", "b": "base-b"}
	got := derive(base, map[string]string{})

	if !maps.Equal(got, base) {
		t.Errorf("derive with empty overrides = %v, want %v", got, base)
	}
}

// TestVariantOverrideKeysExist catches the typo that would otherwise be silent:
// an override written against a key its base does not have adds a message
// nothing ever asks for, and leaves the string it meant to change untouched.
func TestVariantOverrideKeysExist(t *testing.T) {
	for _, v := range variants() {
		for key := range v.overrides {
			if _, ok := v.base[key]; !ok {
				t.Errorf("locale %s overrides key %q, which its base catalog does not define", v.code, key)
			}
		}
	}
}

// TestVariantOverridesActuallyDiffer holds an override to being an override. A
// line that repeats the base verbatim is not a country's departure, it is
// duplication that will silently stop tracking the base the next time the base
// changes.
func TestVariantOverridesActuallyDiffer(t *testing.T) {
	for _, v := range variants() {
		for key, val := range v.overrides {
			if v.base[key] == val {
				t.Errorf("locale %s overrides key %q with the same string its base already has", v.code, key)
			}
		}
	}
}

func TestVariantsCoverEveryBaseKey(t *testing.T) {
	for _, v := range variants() {
		if len(v.messages) != len(v.base) {
			t.Errorf("locale %s has %d keys, want %d (the size of its base)", v.code, len(v.messages), len(v.base))
		}
		for key := range v.base {
			if _, ok := v.messages[key]; !ok {
				t.Errorf("locale %s is missing key %q from its base catalog", v.code, key)
			}
		}
	}
}

// TestEveryDerivedCatalogIsUnderTest checks the list above against the catalog
// map, so a country catalog cannot be shipped without the rules in this file
// applying to it.
func TestEveryDerivedCatalogIsUnderTest(t *testing.T) {
	tested := make(map[string]struct{}, len(variants()))
	for _, v := range variants() {
		tested[v.code] = struct{}{}
	}

	// Every catalog whose file is a derive() call must be listed. These are the
	// locales with no translation file of their own.
	derived := []string{
		"ar-AE", "ar-EG", "bn-IN", "de-AT", "de-CH",
		"en-AU", "en-CA", "en-GB", "en-IN", "en-NZ", "en-ZA",
		"es-AR", "es-MX", "fr-BE", "fr-CA", "fr-CH", "nl-BE", "pt-PT",
	}
	for _, code := range derived {
		if _, ok := catalogs[code]; !ok {
			t.Errorf("catalogs has no entry for derived locale %q", code)
		}
		if _, ok := tested[code]; !ok {
			t.Errorf("derived locale %q is not listed in variants(), so no rule in this file applies to it", code)
		}
	}
}

// TestEsLatamIsNotSelectable holds es-latam to being a shared fragment rather
// than a locale. It is the base es-MX and es-AR build on, not a place anyone
// lives — registering it would offer a country code that is not one.
func TestEsLatamIsNotSelectable(t *testing.T) {
	for _, code := range []string{"es-latam", "es-419"} {
		if _, ok := catalogs[code]; ok {
			t.Errorf("catalogs registers %q, which is a shared fragment and not a selectable locale", code)
		}
		if IsPopulated(code) {
			t.Errorf("IsPopulated(%q) = true, want false", code)
		}
	}
}

// TestEsLatamDepartsFromPeninsularSpanish applies the same two rules to the
// intermediate that variants() applies to every country catalog. It cannot go
// in variants() because it has no catalog entry to check against, but a typo or
// a line that repeats es-ES verbatim is exactly as silent here — and worse,
// because both es-MX and es-AR inherit it.
func TestEsLatamDepartsFromPeninsularSpanish(t *testing.T) {
	for key, val := range esLatamOverrides {
		base, ok := esESMessages[key]
		if !ok {
			t.Errorf("esLatamOverrides overrides key %q, which es-ES does not define", key)
			continue
		}
		if base == val {
			t.Errorf("esLatamOverrides overrides key %q with the same string es-ES already has", key)
		}
	}
}

// TestEsLatamCoversEveryEsESKey checks the intermediate itself carries the full
// peninsular key set, so the two countries deriving from it start complete.
func TestEsLatamCoversEveryEsESKey(t *testing.T) {
	if len(esLatamMessages) != len(esESMessages) {
		t.Errorf("es-latam has %d keys, want %d (the size of es-ES)", len(esLatamMessages), len(esESMessages))
	}
	for key := range esESMessages {
		if _, ok := esLatamMessages[key]; !ok {
			t.Errorf("es-latam is missing key %q from es-ES", key)
		}
	}
}

// TestVariantSpotChecks pins the specific departures the override files were
// written for. If one of these regresses, the locale still has every key and
// still passes every structural test above — and is silently wrong.
func TestVariantSpotChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		locale string
		key    string
		want   string
	}{
		{"British English spells authorisation with an s", "en-GB", MsgAuthMissingToken, "missing authorisation token"},
		{"Australian English inherits British spelling", "en-AU", MsgAuthMissingToken, "missing authorisation token"},
		{"Canadian English keeps the American -ize", "en-CA", MsgAuthMissingToken, "missing authorization token"},
		{"Canadian French calls a repository a dépôt", "fr-CA", MsgAuditAddRepository, "ajouter un dépôt"},
		{"Canadian French uploads with téléverser", "fr-CA", MsgAuditUploadArchive, "téléverser une archive"},
		{"French of France keeps repository", "fr-FR", MsgAuditAddRepository, "ajouter un repository"},
		{"American Spanish says inválida", "es-MX", MsgRepoInvalidURL, "url inválida"},
		{"American Spanish says agregar", "es-AR", MsgAuditAddRepository, "agregar repositorio"},
		{"Peninsular Spanish keeps no válida", "es-ES", MsgRepoInvalidURL, "url no válida"},
		{"European Portuguese says utilizador", "pt-PT", MsgAuditAddGfehPrincipal, "adicionar utilizador de armazenamento de objetos"},
		{"European Portuguese says registo", "pt-PT", MsgAuditAddDNSRecord, "adicionar registo dns"},
		{"Brazilian Portuguese keeps usuário", "pt-BR", MsgAuditAddGfehPrincipal, "adicionar usuário de armazenamento de objetos"},
		{"Egyptian Arabic downloads with تحميل", "ar-EG", MsgAuditDownloadArchive, "تحميل أرشيف"},
		{"Gulf Arabic downloads with تنزيل", "ar-AE", MsgAuditDownloadArchive, "تنزيل أرشيف"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := T(tc.locale, tc.key); got != tc.want {
				t.Errorf("T(%q, %q) = %q, want %q", tc.locale, tc.key, got, tc.want)
			}
		})
	}
}

// TestEveryExtendedLocaleWithACatalogIsAdvertised keeps the three lists in
// step: a locale with a catalog must appear in ExtendedLocales, or the settings
// page will never offer it however complete its translation is.
func TestEveryExtendedLocaleWithACatalogIsAdvertised(t *testing.T) {
	advertised := make(map[string]struct{}, len(ExtendedLocales)+len(CommonLanguages))
	for _, l := range ExtendedLocales {
		advertised[l.Code] = struct{}{}
	}
	for _, l := range CommonLanguages {
		advertised[l.Code] = struct{}{}
	}

	for code := range catalogs {
		if _, ok := advertised[code]; !ok {
			t.Errorf("locale %q has a catalog but appears in neither CommonLanguages nor ExtendedLocales", code)
		}
	}
}

// TestPopulatedLocalesMatchesCatalogs guards the derivation in i18n.go: the
// advertised list is built from the catalog map, and the point of building it
// that way is that the two can never disagree.
func TestPopulatedLocalesMatchesCatalogs(t *testing.T) {
	got := PopulatedLocales()

	if len(got) != len(catalogs) {
		t.Errorf("PopulatedLocales() has %d entries, catalogs has %d", len(got), len(catalogs))
	}
	if len(got) == 0 || got[0] != DefaultLocale {
		t.Errorf("PopulatedLocales()[0] = %q, want the default locale %q", got[0], DefaultLocale)
	}
	for _, code := range got {
		if _, ok := catalogs[code]; !ok {
			t.Errorf("PopulatedLocales() lists %q, which has no catalog", code)
		}
	}
}

// TestPopulatedLocalesReturnsACopy holds the reason PopulatedLocales() clones:
// the list is package state now rather than a fresh literal per call, so a
// caller that sorts or truncates the result must not be able to disturb it.
func TestPopulatedLocalesReturnsACopy(t *testing.T) {
	first := PopulatedLocales()
	if len(first) == 0 {
		t.Fatal("PopulatedLocales() is empty")
	}
	first[0] = "mutated"

	if second := PopulatedLocales(); second[0] != DefaultLocale {
		t.Errorf("mutating the result changed the next call: got %q, want %q", second[0], DefaultLocale)
	}
}
