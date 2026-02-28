/** @import { AuditListOptions, AuditPage } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Queries the audit log with cursor-based or offset pagination, account
 * filtering, sorting, and search. All administrative actions are recorded
 * including storage, repository, package, account, and settings operations.
 *
 * Calls POST /audit/log on the Control Plane Service.
 *
 * @param {AuditListOptions} opts - filtering and pagination options. Supports
 *   cursor (string), offset (number), limit (number, default 50, max 200),
 *   account (string filter), sort_by (string), sort_dir ("asc"|"desc"), and
 *   search (string).
 * @returns {Promise<AuditPage>}
 */
SystemControllerClient.prototype.listAuditLog = async function (opts) {
  return this.postJSON('/audit/log', opts)
}
