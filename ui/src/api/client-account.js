/** @import { Account, UpdateFields } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Create a new user account.
 * @param {string} username - Unique username.
 * @param {string} password - Password (must be at least 8 characters).
 * @param {string} email - Email address (validated against standard format).
 * @param {string} phone - Phone number (digits with optional formatting).
 * @param {string} realName - Display name.
 * @param {boolean} admin - When true, the account receives administrator privileges. Ignored when `wireguard` is true.
 * @param {boolean} [wireguard] - When true, create a WireGuard-only account scoped to `networks` instead of a normal account.
 * @param {string[]} [networks] - Networks the WireGuard-only account may enroll peers on. Must be non-empty when `wireguard` is true.
 * @returns {Promise<Account>}
 */
SystemControllerClient.prototype.createAccount = async function (username, password, email, phone, realName, admin, wireguard = false, networks = []) {
  return this.postJSON('/account/create', {
    username,
    password,
    email,
    phone,
    real_name: realName,
    admin,
    wireguard,
    networks,
  })
}

/**
 * @param {string} username
 * @returns {Promise<Account>}
 */
SystemControllerClient.prototype.getAccount = async function (username) {
  return this.postJSON('/account', { username })
}

/**
 * Update fields on an existing account. Only provided fields are changed.
 * @param {string} username - Account to update.
 * @param {UpdateFields} fields - Fields to change (password, email, phone, real_name, admin).
 * @returns {Promise<Account>}
 */
SystemControllerClient.prototype.updateAccount = async function (username, fields) {
  return this.postJSON('/account/update', { username, fields })
}

/**
 * @param {string} [sortBy]
 * @param {string} [sortOrder]
 * @param {number} [limit]
 * @param {number} [offset]
 * @param {string} [search]
 * @returns {Promise<{entries: Account[], has_more: boolean, total_pages: number, total_count: number}>}
 */
SystemControllerClient.prototype.listAccounts = async function (sortBy, sortOrder, limit, offset, search) {
  const params = new URLSearchParams()
  if (sortBy) params.set('sort_by', sortBy)
  if (sortOrder) params.set('sort_order', sortOrder)
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (search) params.set('search', search)
  const qs = params.toString()
  return this.getJSON(`/account${qs ? `?${qs}` : ''}`)
}

/**
 * @param {string} username
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.disableAccount = async function (username) {
  await this.post('/account/disable', { username })
}

/**
 * @param {string} username
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.enableAccount = async function (username) {
  await this.post('/account/enable', { username })
}
