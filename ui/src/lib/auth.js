/** @import { Account } from '../api/types.js' */

const TOKEN_KEY = 'town-os-token'
const ACCOUNT_KEY = 'town-os-account'

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
