import { SystemControllerClient } from './core.js'

/**
 * Returns the current state of the monitoring stack (Prometheus, Node Exporter,
 * Grafana) including container running status, images, and ports.
 *
 * Calls GET /monitoring/status on the Control Plane Service.
 *
 * @returns {Promise<{prometheus: {name: string, image: string, running: boolean, port: string}, node_exporter: {name: string, image: string, running: boolean, port: string}, grafana: {name: string, image: string, running: boolean, port: string}}>}
 */
SystemControllerClient.prototype.monitoringStatus = async function () {
  return this.getJSON('/monitoring/status')
}
