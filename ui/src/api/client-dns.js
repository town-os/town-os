import { SystemControllerClient } from './core.js'

/**
 * Returns the current DNS service status.
 *
 * Calls GET /dns/status on the Control Plane Service.
 *
 * @returns {Promise<{enabled: boolean, running: boolean, tld: string, record_count: number}>}
 */
SystemControllerClient.prototype.dnsStatus = async function () {
  return this.getJSON('/dns/status')
}

/**
 * Returns the list of DNS records.
 *
 * Calls GET /dns/records on the Control Plane Service.
 *
 * @returns {Promise<Array<{name: string, record_type: number, value: string, ttl: number, priority: number}>>}
 */
SystemControllerClient.prototype.listDNSRecords = async function () {
  return this.getJSON('/dns/records')
}

/**
 * Adds a new DNS record.
 *
 * Calls POST /dns/records/add on the Control Plane Service.
 *
 * @param {string} name - The record name (e.g. "app.example.town").
 * @param {number} recordType - The record type as protobuf enum integer.
 * @param {string} value - The record value.
 * @param {number} ttl - Time to live in seconds.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.addDNSRecord = async function (name, recordType, value, ttl) {
  await this.postJSON('/dns/records/add', { name, record_type: recordType, value, ttl })
}

/**
 * Removes a DNS record.
 *
 * Calls POST /dns/records/remove on the Control Plane Service.
 *
 * @param {string} name - The record name.
 * @param {number} recordType - The record type as protobuf enum integer.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeDNSRecord = async function (name, recordType) {
  await this.postJSON('/dns/records/remove', { name, record_type: recordType })
}

/**
 * Returns the current DNS TLD.
 *
 * Calls GET /dns/tld on the Control Plane Service.
 *
 * @returns {Promise<{tld: string}>}
 */
SystemControllerClient.prototype.getDNSTLD = async function () {
  return this.getJSON('/dns/tld')
}

/**
 * Sets the DNS TLD.
 *
 * Calls POST /dns/tld on the Control Plane Service.
 *
 * @param {string} tld - The new TLD to set.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setDNSTLD = async function (tld) {
  await this.postJSON('/dns/tld', { tld })
}

/**
 * Runs DNS setup (idempotent zone init + package registration).
 *
 * Calls POST /dns/setup on the Control Plane Service.
 *
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setupDNS = async function () {
  await this.postJSON('/dns/setup', {})
}
