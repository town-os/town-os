/**
 * Build a country-variant catalog from the catalog of the language it belongs
 * to, plus the strings that genuinely differ in that country.
 *
 * A regional catalog is not a translation. Austrian German is German; the
 * question a de-AT catalog answers is not "how do you say this in German" but
 * "which of these sentences would an Austrian not have written". Copying
 * de-DE into de-AT.js and editing four lines would mean the next key added to
 * de-DE silently reaches Austria in English, and a fix to a German string has
 * to be found and repeated in three files. Inheriting the base and listing
 * only the departures keeps a variant correct by default.
 *
 * Some override maps are small and a few are empty — that is the honest answer
 * for a technical control panel. An empty one still marks the locale as
 * deliberately reviewed rather than forgotten.
 *
 * Mirrors `derive` in src/i18n/derive.go.
 *
 * @param {Record<string, string>} base - The language catalog to inherit.
 * @param {Record<string, string>} overrides - Strings this country states
 *   differently. Every key must exist in base; `derive.test.js` holds that.
 * @returns {Record<string, string>} A new catalog; neither argument is modified.
 */
export default function derive(base, overrides) {
  return { ...base, ...overrides }
}
