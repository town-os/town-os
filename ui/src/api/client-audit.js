/** @import { AuditListOptions, AuditPage } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * @param {AuditListOptions} opts
 * @returns {Promise<AuditPage>}
 */
SystemControllerClient.prototype.listAuditLog = async function (opts) {
  return this.postJSON('/audit/log', opts)
}
