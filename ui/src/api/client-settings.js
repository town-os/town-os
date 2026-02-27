import { SystemControllerClient } from './core.js'

/** @returns {Promise<Record<string, string>>} */
SystemControllerClient.prototype.getSettings = async function () {
  return this.getJSON('/settings')
}

/**
 * @param {string} key
 * @returns {Promise<string>}
 */
SystemControllerClient.prototype.getSetting = async function (key) {
  /** @type {{ value: string }} */
  const result = await this.postJSON('/settings/get', { key })
  return result.value
}

/**
 * @param {string} key
 * @param {string} value
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setSetting = async function (key, value) {
  await this.post('/settings/set', { key, value })
}
