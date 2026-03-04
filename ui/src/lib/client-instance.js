import { SystemControllerClient } from '../api/client.js'
import { getToken } from './auth.js'

export function getBaseURLForPort(port) {
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL
  }
  return `${window.location.protocol}//${window.location.hostname}:${port}`
}

function getBaseURL() {
  return getBaseURLForPort(5309)
}

const client = new SystemControllerClient(getBaseURL())

/** @returns {SystemControllerClient} */
export default function getClient() {
  const token = getToken()
  if (token) {
    client.setToken(token)
  }
  return client
}
