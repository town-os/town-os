import { SystemControllerClient } from './core.js'

/**
 * @typedef {object} Locale
 * @property {string} code - BCP 47 locale code (e.g. "en-US").
 * @property {string} native_name - Name in native script (e.g. "English").
 * @property {string} english_name - Name in English (e.g. "English").
 */

/**
 * @typedef {object} LocaleListResponse
 * @property {string} current - The currently active locale code.
 * @property {string[]} populated - Locale codes that have translations available.
 * @property {Locale[]} common_languages - Curated list of common languages.
 * @property {Locale[]} extended_locales - Full list of country-specific locale codes.
 */

/**
 * Returns the available locales and the currently selected locale.
 *
 * Calls GET /locales on the Control Plane Service.
 *
 * @returns {Promise<LocaleListResponse>}
 */
SystemControllerClient.prototype.getLocales = async function () {
  return this.getJSON('/locales')
}
