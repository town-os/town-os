import { SystemControllerClient } from './core.js'

/**
 * Lists all networks with peer counts and running state.
 *
 * Calls GET /networks on the Control Plane Service.
 *
 * @returns {Promise<Array<{name: string, tld: string, subnet: string, address: string, public_key: string, listen_port: number, enabled: boolean, peer_count: number, interface: string, running: boolean}>>}
 */
SystemControllerClient.prototype.listNetworks = async function () {
  return this.getJSON('/networks')
}

/**
 * Creates a new network. The server derives the overlay subnet from the
 * systemd machine-id and generates a WireGuard keypair.
 *
 * Calls POST /networks/create.
 *
 * @param {string} name - Lowercase alphanumeric-with-dashes network name.
 * @param {string} [tld] - Optional TLD; defaults to the network name.
 * @returns {Promise<object>}
 */
SystemControllerClient.prototype.createNetwork = async function (name, tld) {
  return this.postJSON('/networks/create', { name, tld })
}

/**
 * Removes a network (the default "home" network cannot be removed).
 *
 * Calls POST /networks/remove.
 *
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeNetwork = async function (name) {
  await this.postJSON('/networks/remove', { name })
}

/**
 * Enables a network (brings its WireGuard interface up → remote access on).
 *
 * Calls POST /networks/enable.
 *
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.enableNetwork = async function (name) {
  await this.postJSON('/networks/enable', { name })
}

/**
 * Disables a network (brings its WireGuard interface down → remote access off;
 * local DNS and services keep running).
 *
 * Calls POST /networks/disable.
 *
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.disableNetwork = async function (name) {
  await this.postJSON('/networks/disable', { name })
}

/**
 * Lists the peers on a network.
 *
 * Calls GET /networks/peers?network=<name>.
 *
 * @param {string} network
 * @returns {Promise<Array<{network: string, public_key: string, name: string, allowed_ip: string, endpoint: string}>>}
 */
SystemControllerClient.prototype.listNetworkPeers = async function (network) {
  return this.getJSON(`/networks/peers?network=${encodeURIComponent(network)}`)
}

/**
 * Adds a peer to a network. When public_key is omitted the server generates a
 * keypair and returns the private key plus a ready-to-import device config.
 *
 * Calls POST /networks/peers/add.
 *
 * @param {string} network
 * @param {string} name - Human-readable device label.
 * @param {string} [publicKey] - The device's own public key (optional).
 * @param {string} [endpoint] - The device's reachable endpoint (optional).
 * @returns {Promise<{peer: object, private_key?: string, config: string}>}
 */
SystemControllerClient.prototype.addNetworkPeer = async function (network, name, publicKey, endpoint) {
  return this.postJSON('/networks/peers/add', { network, name, public_key: publicKey, endpoint })
}

/**
 * Removes a peer from a network.
 *
 * Calls POST /networks/peers/remove.
 *
 * @param {string} network
 * @param {string} publicKey
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeNetworkPeer = async function (network, publicKey) {
  await this.postJSON('/networks/peers/remove', { network, public_key: publicKey })
}
