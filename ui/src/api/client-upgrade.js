/** @import { PackageUpgrade } from './types.js' */
import { SystemControllerClient } from './core.js'

/** @returns {Promise<PackageUpgrade[]>} */
SystemControllerClient.prototype.listUpgrades = async function () {
  return this.getJSON('/packages/upgrades')
}

/** @returns {Promise<void>} */
SystemControllerClient.prototype.dismissUpgrades = async function () {
  await this.post('/packages/upgrades/dismiss', {})
}
