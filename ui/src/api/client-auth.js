/** @import { AuthenticateResponse, Session } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * @param {string} username
 * @param {string} password
 * @returns {Promise<AuthenticateResponse>}
 */
SystemControllerClient.prototype.authenticate = async function (username, password) {
  return this.postJSON('/account/authenticate', { username, password })
}

/**
 * @param {string} token
 * @returns {Promise<Session[]>}
 */
SystemControllerClient.prototype.listSessions = async function (token) {
  return this.getJSON('/account/sessions', {
    Authorization: `Bearer ${token}`,
  })
}

/**
 * @param {string} token
 * @returns {Promise<string>}
 */
SystemControllerClient.prototype.sessionUsername = async function (token) {
  /** @type {{ username: string }} */
  const result = await this.getJSON('/account/me', {
    Authorization: `Bearer ${token}`,
  })
  return result.username
}

/**
 * @param {string} sessionID
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.revokeSession = async function (sessionID) {
  await this.post('/account/session/revoke', { session_id: sessionID })
}
