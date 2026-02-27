/** @import { PingResponse } from './types.js' */
import { SystemControllerClient } from './core.js'

/** @returns {Promise<PingResponse>} */
SystemControllerClient.prototype.ping = async function () {
  return this.getJSON('/status/ping')
}
