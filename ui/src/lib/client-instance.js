import { SystemControllerClient } from '../api/client.js'
import { getToken } from './auth.js'

function getBaseURL() {
  if (import.meta.env.VITE_CLIENT_URL) {
    return import.meta.env.VITE_CLIENT_URL
  }
  return ''
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
