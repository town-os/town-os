/** @import { PingResponse } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Returns system status including filesystem counts, repository and package
 * counts, service unit counts, disk usage, upgrade availability, and the
 * authenticated username if a valid token is provided.
 *
 * Calls GET /status/ping on the Control Plane Service.
 *
 * @returns {Promise<PingResponse>}
 */
SystemControllerClient.prototype.ping = async function () {
  return this.getJSON('/status/ping')
}
