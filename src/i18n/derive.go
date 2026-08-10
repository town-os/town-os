package i18n

import "maps"

// derive builds a country-variant catalog from the catalog of the language it
// belongs to, plus the strings that genuinely differ in that country.
//
// A regional catalog is not a translation. Austrian German is German; the
// question a de-AT catalog answers is not "how do you say this in German"
// but "which of these sentences would an Austrian not have written". Copying
// de-DE into de_at.go and editing four lines would mean that the next message
// key added to de-DE silently reaches Austria in English, and that a fix to a
// German string has to be found and repeated in three files. Inheriting the
// base and listing only the departures keeps a variant correct by default:
// a new key lands everywhere the moment its base language has it, and a
// country only overrides what it actually says differently.
//
// Some override maps are small and a few are empty. That is the honest answer
// for a technical control panel — Canadian English keeps the -ize spellings
// American English uses, so en-CA departs from en-US in nothing this catalog
// says. An empty override map still buys something: it marks the locale as
// deliberately reviewed and populated rather than forgotten.
//
// Parameters:
//   - base: the language catalog this variant inherits from (e.g. deDEMessages).
//   - overrides: the strings this country states differently. Keys must exist
//     in base; TestVariantOverrideKeysExist holds that.
//
// Returns a new catalog carrying every key of base with overrides applied.
// Neither argument is modified.
func derive(base, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base))
	maps.Copy(out, base)
	maps.Copy(out, overrides)
	return out
}
