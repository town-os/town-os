import { SystemControllerClient } from './core.js'

/**
 * Returns all system settings as a key-value map. Default settings include
 * "default_quota" (50 GB), "max_archive_size" (20 MB),
 * "archive_unpack_timeout" (120 seconds), and "proton_image" (container
 * image for Valve's Proton compatibility layer, empty by default).
 *
 * Calls GET /settings on the Control Plane Service.
 *
 * @returns {Promise<Record<string, string>>}
 */
SystemControllerClient.prototype.getSettings = async function () {
  return this.getJSON('/settings')
}

/**
 * Returns the value of a single system setting by key.
 *
 * Calls POST /settings/get on the Control Plane Service.
 *
 * @param {string} key - the setting key to retrieve. Valid keys:
 *   "default_quota" (bytes, default 50 GB), "max_archive_size" (bytes,
 *   default 20 MB), "archive_unpack_timeout" (seconds, default 120),
 *   "proton_image" (Proton runner container image, empty by default).
 * @returns {Promise<string>}
 */
SystemControllerClient.prototype.getSetting = async function (key) {
  /** @type {{ value: string }} */
  const result = await this.postJSON('/settings/get', { key })
  return result.value
}

/**
 * Creates or updates a system setting. Byte-value settings ("default_quota",
 * "max_archive_size") accept human-readable strings such as "500GB" or "10MB",
 * which are parsed and stored as numeric byte counts. The "proton_image"
 * setting accepts a container image reference for Valve's Proton runner.
 *
 * Calls POST /settings/set on the Control Plane Service.
 *
 * @param {string} key - the setting key to set.
 * @param {string} value - the new value. For byte-value settings, accepts
 *   human-readable strings (e.g., "50GB", "20MB"). For "archive_unpack_timeout",
 *   the value is stored as a number of seconds.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setSetting = async function (key, value) {
  await this.post('/settings/set', { key, value })
}
