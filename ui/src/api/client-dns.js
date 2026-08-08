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
 * Returns DNS records annotated with their network and TLD. With no `tld` it
 * returns records across every network (the global home zone plus each
 * network's scoped zone); a non-empty `tld` restricts the result to that domain.
 *
 * Calls GET /dns/records[?tld=<tld>] on the Control Plane Service.
 *
 * @param {string} [tld] - Optional TLD to filter by; omit for all networks.
 * @returns {Promise<Array<{name: string, record_type: number, value: string, ttl: number, priority: number, network: string, tld: string}>>}
 */
SystemControllerClient.prototype.listDNSRecords = async function (tld) {
  const query = tld ? `?tld=${encodeURIComponent(tld)}` : ''
  return this.getJSON(`/dns/records${query}`)
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
 *
 * Each provider's `refusal_codes` come back RESOLVED — a provider that names
 * none reads back as rolodex's built-in set rather than as an empty list, so the
 * screen can show what the box is actually matching on. `rotated_out` lists the
 * providers currently not being asked because they refused a query.
 *
 * Calls GET /dns/rbl.
 * @returns {Promise<{enabled: boolean, providers: Array<{zone: string, enabled: boolean, refusal_codes?: string[], refusal_cooldown_secs?: number}>, refusal_cooldown_secs?: number, rotated_out?: Array<{zone: string, code: string, seconds_remaining: number}>}>}
 */
SystemControllerClient.prototype.getRBLConfig = async function () {
  return this.getJSON('/dns/rbl')
}

/**
 * Replaces the RBL configuration.
 *
 * A provider's `refusal_codes` say what "I refused your query" looks like coming
 * back from that provider, as opposed to "this is listed" — the two arrive as
 * the same kind of record and only the address separates them. Omit the field
 * (or pass an empty array) to use rolodex's built-in set; pass `['none']` to
 * switch the detection off for a list whose real listings collide with one.
 *
 * Calls POST /dns/rbl.
 * @param {boolean} enabled
 * @param {Array<{zone: string, enabled: boolean, refusal_codes?: string[], refusal_cooldown_secs?: number}>} providers
 * @param {number} [refusalCooldownSecs] seconds a refusing provider is taken out
 *   of the lookup rotation, for providers that set none of their own; 0 or
 *   omitted uses rolodex's built-in default
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setRBLConfig = async function (enabled, providers, refusalCooldownSecs = 0) {
  await this.postJSON('/dns/rbl', { enabled, providers, refusal_cooldown_secs: refusalCooldownSecs })
}

/**
 * Returns the DNSBL (domain blocklist) configuration, in the same shape as
 * [getRBLConfig]{@link SystemControllerClient#getRBLConfig}.
 *
 * Calls GET /dns/dnsbl.
 * @returns {Promise<{enabled: boolean, providers: Array<{zone: string, enabled: boolean, refusal_codes?: string[], refusal_cooldown_secs?: number}>, refusal_cooldown_secs?: number, rotated_out?: Array<{zone: string, code: string, seconds_remaining: number}>}>}
 */
SystemControllerClient.prototype.getDNSBLConfig = async function () {
  return this.getJSON('/dns/dnsbl')
}

/**
 * Replaces the DNSBL configuration. Refusal handling works exactly as in
 * [setRBLConfig]{@link SystemControllerClient#setRBLConfig}, with its own
 * independent cooldown.
 *
 * Calls POST /dns/dnsbl.
 * @param {boolean} enabled
 * @param {Array<{zone: string, enabled: boolean, refusal_codes?: string[], refusal_cooldown_secs?: number}>} providers
 * @param {number} [refusalCooldownSecs]
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setDNSBLConfig = async function (enabled, providers, refusalCooldownSecs = 0) {
  await this.postJSON('/dns/dnsbl', { enabled, providers, refusal_cooldown_secs: refusalCooldownSecs })
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

// --- DNSBL allowlist ---

/**
 * Lists the DNSBL allowlist entries — names exempted from the name-based
 * blocklist check, along with every name beneath them.
 * Calls GET /dns/dnsbl/allowlist.
 * @returns {Promise<Array<{name: string, reason: string}>>}
 */
SystemControllerClient.prototype.listDNSBLAllowlist = async function () {
  return this.getJSON('/dns/dnsbl/allowlist')
}

/**
 * Exempts a domain (and every name under it) from the name-based blocklist
 * check, overriding both the provider zones and the local blocklist.
 * Calls POST /dns/dnsbl/allowlist/add.
 * @param {string} name
 * @param {string} reason
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.addDNSBLAllowlist = async function (name, reason) {
  await this.postJSON('/dns/dnsbl/allowlist/add', { name, reason })
}

/**
 * Removes a domain from the DNSBL allowlist.
 * Calls POST /dns/dnsbl/allowlist/remove.
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeDNSBLAllowlist = async function (name) {
  await this.postJSON('/dns/dnsbl/allowlist/remove', { name })
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
