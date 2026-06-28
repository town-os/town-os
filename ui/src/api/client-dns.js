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

// --- RBL / DNSBL ---

/**
 * Returns the RBL (reverse-IP blocklist) configuration.
 * Calls GET /dns/rbl.
 * @returns {Promise<{enabled: boolean, providers: Array<{zone: string, enabled: boolean}>}>}
 */
SystemControllerClient.prototype.getRBLConfig = async function () {
  return this.getJSON('/dns/rbl')
}

/**
 * Replaces the RBL configuration.
 * Calls POST /dns/rbl.
 * @param {boolean} enabled
 * @param {Array<{zone: string, enabled: boolean}>} providers
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setRBLConfig = async function (enabled, providers) {
  await this.postJSON('/dns/rbl', { enabled, providers })
}

/**
 * Returns the DNSBL (domain blocklist) configuration.
 * Calls GET /dns/dnsbl.
 * @returns {Promise<{enabled: boolean, providers: Array<{zone: string, enabled: boolean}>}>}
 */
SystemControllerClient.prototype.getDNSBLConfig = async function () {
  return this.getJSON('/dns/dnsbl')
}

/**
 * Replaces the DNSBL configuration.
 * Calls POST /dns/dnsbl.
 * @param {boolean} enabled
 * @param {Array<{zone: string, enabled: boolean}>} providers
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setDNSBLConfig = async function (enabled, providers) {
  await this.postJSON('/dns/dnsbl', { enabled, providers })
}

/**
 * Lists the local RBL blocklist entries.
 * Calls GET /dns/rbl/local.
 * @returns {Promise<Array<{name: string, reason: string}>>}
 */
SystemControllerClient.prototype.listLocalRBL = async function () {
  return this.getJSON('/dns/rbl/local')
}

/**
 * Adds a local RBL blocklist entry (domain or IP).
 * Calls POST /dns/rbl/local/add.
 * @param {string} name
 * @param {string} reason
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.addLocalRBL = async function (name, reason) {
  await this.postJSON('/dns/rbl/local/add', { name, reason })
}

/**
 * Removes a local RBL blocklist entry.
 * Calls POST /dns/rbl/local/remove.
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeLocalRBL = async function (name) {
  await this.postJSON('/dns/rbl/local/remove', { name })
}

// --- DNS services (publish toggle) ---

/**
 * Lists installed package services and their published (in-DNS-zone) state.
 * Calls GET /dns/services.
 * @returns {Promise<Array<{repo: string, name: string, version: string, fqdn: string, domains: string[], published: boolean}>>}
 */
SystemControllerClient.prototype.listDNSServices = async function () {
  return this.getJSON('/dns/services')
}

/**
 * Publishes or unpublishes a package service in the DNS zone.
 * Calls POST /dns/services/set.
 * @param {string} repo
 * @param {string} name
 * @param {boolean} published
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setDNSService = async function (repo, name, published) {
  await this.postJSON('/dns/services/set', { repo, name, published })
}
