import { SystemControllerClient } from './core.js'

/**
 * Returns the list of system services with their current status.
 *
 * Calls GET /system-services on the Control Plane Service.
 *
 * @returns {Promise<Array<{key: string, display_name: string, image: string, port: string, Name: string, ActiveState: string, SubState: string}>>}
 */
SystemControllerClient.prototype.listSystemServices = async function () {
  return this.getJSON('/system-services')
}

/**
 * Applies an action (start, stop, restart) to a system service.
 *
 * Calls POST /system-services/status on the Control Plane Service.
 *
 * @param {string} key - The system service key (e.g. "prometheus").
 * @param {string} action - The action to apply ("start", "stop", "restart").
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setSystemServiceStatus = async function (key, action) {
  await this.postJSON('/system-services/status', { key, action })
}

/**
 * Pulls latest container images and restarts all core services.
 *
 * Calls POST /system-services/refresh on the Control Plane Service.
 *
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.refreshSystemServices = async function () {
  await this.postJSON('/system-services/refresh', {})
}
