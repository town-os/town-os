import { SystemControllerClient } from './core.js'

// Object storage (gfeh).
//
// One partition per network. gfehd's administrative surface is a Unix socket
// that checks no credential, so none of this talks to it directly — the system
// controller proxies, authenticating the caller the way it does everywhere
// else.

/**
 * Lists every object-storage partition with its published names and live state.
 *
 * Calls GET /gfeh. Answers an empty array when object storage is not
 * configured, which is a deployment choice rather than a failure.
 *
 * @returns {Promise<Array<{network: string, tld: string, quota: number, running: boolean, names: Array<{view: string, fqdn: string, port: number, http: boolean}>}>>}
 */
SystemControllerClient.prototype.listGfehPartitions = async function () {
  return this.getJSON('/gfeh')
}

/**
 * Lists the principals in a partition's ACL forest.
 *
 * `account: true` marks a principal that corresponds to a Town OS account, as
 * opposed to a sub-principal created inside gfeh.
 *
 * Calls GET /gfeh/principals.
 *
 * @param {string} network
 * @returns {Promise<Array<{name: string, parent?: string, ceiling: string[], account: boolean}>>}
 */
SystemControllerClient.prototype.listGfehPrincipals = async function (network) {
  return this.getJSON(`/gfeh/principals?network=${encodeURIComponent(network)}`)
}

/**
 * Projects a Town OS account into a partition.
 *
 * The ceiling follows the account: an administrator becomes a gfeh superuser,
 * anyone else gets read-write and no grants — which is deliberately useless
 * until somebody grants them something.
 *
 * Calls POST /gfeh/principals/add (admin).
 *
 * @param {string} network
 * @param {string} principal - An existing Town OS username.
 * @returns {Promise<{name: string, ceiling: string[], account: boolean}>}
 */
SystemControllerClient.prototype.addGfehPrincipal = async function (network, principal) {
  return this.postJSON('/gfeh/principals/add', { network, principal })
}

/**
 * Removes a principal, and with it every grant derived from it.
 *
 * Calls POST /gfeh/principals/remove (admin).
 *
 * @param {string} network
 * @param {string} principal
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeGfehPrincipal = async function (network, principal) {
  await this.postJSON('/gfeh/principals/remove', { network, principal })
}

/**
 * Lists what one principal holds in a partition.
 *
 * principal is required: gfehd's own handler requires it, so an absent one is
 * an error rather than "every grant".
 *
 * Calls GET /gfeh/grants.
 *
 * @param {string} network
 * @param {string} principal
 * @returns {Promise<Array<{id: number, principal: string, path: string, perm: string[], inheritable: boolean}>>}
 */
SystemControllerClient.prototype.listGfehGrants = async function (network, principal) {
  const q = `network=${encodeURIComponent(network)}&principal=${encodeURIComponent(principal)}`
  return this.getJSON(`/gfeh/grants?${q}`)
}

/**
 * Grants a principal authority over a subtree.
 *
 * The response carries the rights as *stored*, which is what gfeh clamped them
 * to rather than what was asked for. Render that, not the request — an
 * administrator has to be able to see a grant was narrowed, or they will
 * believe they gave access nobody has.
 *
 * Calls POST /gfeh/grants/add (admin).
 *
 * @param {string} network
 * @param {string} principal
 * @param {string} path - Relative to the partition root.
 * @param {string[]} perm
 * @param {boolean} inheritable - Whether it extends beneath path.
 * @returns {Promise<{id: number, principal: string, path: string, perm: string[], inheritable: boolean}>}
 */
SystemControllerClient.prototype.addGfehGrant = async function (network, principal, path, perm, inheritable) {
  return this.postJSON('/gfeh/grants/add', { network, principal, path, perm, inheritable })
}

/**
 * Revokes a grant by id.
 *
 * Calls POST /gfeh/grants/revoke (admin).
 *
 * @param {string} network
 * @param {number} id
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.revokeGfehGrant = async function (network, id) {
  await this.postJSON('/gfeh/grants/revoke', { network, id })
}

/**
 * Lists a partition's published links.
 *
 * Calls GET /gfeh/exposures.
 *
 * @param {string} network
 * @returns {Promise<Array<{token: string, path: string, filename?: string, enabled: boolean}>>}
 */
SystemControllerClient.prototype.listGfehExposures = async function (network) {
  return this.getJSON(`/gfeh/exposures?network=${encodeURIComponent(network)}`)
}

/**
 * Withdraws a published link, after which it stops resolving.
 *
 * Calls POST /gfeh/exposures/withdraw (admin).
 *
 * @param {string} network
 * @param {string} token
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.withdrawGfehExposure = async function (network, token) {
  await this.postJSON('/gfeh/exposures/withdraw', { network, token })
}
