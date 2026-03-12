/** @import { Account } from '../api/types.js' */

const TOKEN_KEY = 'town-os-token'
const ACCOUNT_KEY = 'town-os-account'
const SESSION_EXPIRED_KEY = 'town-os-session-expired'

/** @returns {string | null} */
export function getToken() {
  return localStorage.getItem(TOKEN_KEY)
}

/** @param {string} token */
export function setToken(token) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(ACCOUNT_KEY)
}

/** @returns {Account | null} */
export function getAccount() {
  const raw = localStorage.getItem(ACCOUNT_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

/** @param {Account} account */
export function setAccount(account) {
  localStorage.setItem(ACCOUNT_KEY, JSON.stringify(account))
}

/** Mark that the session expired so the login page can show a message. */
export function setSessionExpired() {
  sessionStorage.setItem(SESSION_EXPIRED_KEY, '1')
}

/**
 * Check and clear the session-expired flag.
 * @returns {boolean}
 */
export function getAndClearSessionExpired() {
  const val = sessionStorage.getItem(SESSION_EXPIRED_KEY)
  if (val) {
    sessionStorage.removeItem(SESSION_EXPIRED_KEY)
    return true
  }
  return false
}
