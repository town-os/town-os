/** @import { AuthenticateResponse, Session } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Validates credentials and returns a session token on success. The token is a
 * JWT (HS256) that expires after 7 days from last use.
 *
 * Calls POST /account/authenticate on the Control Plane Service.
 *
 * @param {string} username - the account username.
 * @param {string} password - the account password (minimum 8 characters).
 * @returns {Promise<AuthenticateResponse>}
 */
SystemControllerClient.prototype.authenticate = async function (username, password) {
  return this.postJSON('/account/authenticate', { username, password })
}

/**
 * Returns all active sessions for the user identified by the given bearer token.
 *
 * Calls GET /account/sessions on the Control Plane Service.
 *
 * @param {string} token - a valid JWT bearer token identifying the user.
 * @returns {Promise<Session[]>}
 */
SystemControllerClient.prototype.listSessions = async function (token) {
  return this.getJSON('/account/sessions', {
    Authorization: `Bearer ${token}`,
  })
}

/**
 * Returns the username associated with the given session token.
 *
 * Calls GET /account/me on the Control Plane Service.
 *
 * @param {string} token - a valid JWT bearer token.
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
 * Invalidates a session by its ID, preventing further use of the associated token.
 *
 * Calls POST /account/session/revoke on the Control Plane Service.
 *
 * @param {string} sessionID - the UUID of the session to revoke.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.revokeSession = async function (sessionID) {
  await this.post('/account/session/revoke', { session_id: sessionID })
}
