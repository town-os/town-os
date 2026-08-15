import { SystemControllerClient } from './core.js'

/**
 * Returns the current state of the monitoring stack (Prometheus, Node Exporter,
 * Grafana) including container running status, images, and ports.
 *
 * It also carries Prometheus's own view of every scrape job: scrape_targets is
 * the full list with each failing target's error, down_jobs is just the names
 * of the broken ones, and scrape_targets_error explains an empty list on a box
 * where Prometheus is up. That last distinction matters — a job that has never
 * scraped leaves every unit active and every panel empty, so "could not ask"
 * and "nothing is wrong" must never render the same.
 *
 * Calls GET /monitoring/status on the Control Plane Service.
 *
 * @returns {Promise<{backend: string, prometheus: boolean, node_exporter: boolean, monitoring_ui: boolean, grafana?: boolean, disk_devices?: string[], scrape_targets?: {job: string, instance: string, health: string, last_error?: string, scrape_url?: string}[], down_jobs?: string[], scrape_targets_error?: string}>}
 */
SystemControllerClient.prototype.monitoringStatus = async function () {
  return this.getJSON('/monitoring/status')
}
