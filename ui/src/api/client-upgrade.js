/** @import { PackageUpgrade } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Lists available package upgrades. Each entry includes repo, name, installed
 * version, latest version, and a changed flag indicating local modifications.
 *
 * Calls GET /packages/upgrades on the Control Plane Service.
 *
 * @returns {Promise<PackageUpgrade[]>}
 */
SystemControllerClient.prototype.listUpgrades = async function () {
  return this.getJSON('/packages/upgrades')
}

/**
 * Marks current upgrades as dismissed by computing a SHA256 hash of the current
 * upgrade set and storing it as the dismissed_upgrades_hash setting.
 *
 * Calls POST /packages/upgrades/dismiss on the Control Plane Service.
 *
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.dismissUpgrades = async function () {
  await this.post('/packages/upgrades/dismiss', {})
}
