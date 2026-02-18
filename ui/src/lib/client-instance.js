import { SystemControllerClient } from '../api/client.js'
import { getToken } from './auth.js'

function getBaseURL() {
  if (import.meta.env.VITE_CLIENT_URL) {
    return import.meta.env.VITE_CLIENT_URL
  }
  if (typeof window !== 'undefined' && window.location && window.location.hostname) {
    return `${window.location.protocol}//${window.location.hostname}:8080`
  }
  return 'http://localhost:8080'
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
