import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import getClient from './client-instance.js'
import { getToken, clearToken, getAccount, setSessionExpired } from './auth.js'
import { ApiError } from '../api/client.js'

/**
 * Hook to poll an API call periodically and maintain state.
 * Uses a generation counter to discard stale responses from in-flight
 * requests that complete after deps have changed.
 * @template T
 * @param {() => Promise<T>} fetcher
 * @param {T} defaultValue
 * @param {any[]} [deps]
 * @param {number} [interval]
 * @returns {[T, () => void, boolean]}
 */
export function usePolling(fetcher, defaultValue, deps = [], interval = 60000) {
  const [data, setData] = useState(defaultValue)
  const [loading, setLoading] = useState(true)
  const generationRef = useRef(0)

  const refresh = useCallback(() => {
    const gen = ++generationRef.current
    setLoading(true)
    fetcher()
      .then((result) => {
        if (gen !== generationRef.current) return
        setData(result)
        setLoading(false)
      })
      .catch((err) => {
        if (gen !== generationRef.current) return
        console.debug('usePolling fetch error:', err)
        setLoading(false)
      })
  }, deps)

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, interval)
    return () => clearInterval(id)
  }, [refresh, interval])

  return [data, refresh, loading]
}

/**
 * Hook that verifies the session is still valid, redirecting to login if not.
 */
export function useRequireAuth() {
  const navigate = useNavigate()

  useEffect(() => {
    const token = getToken()
    if (!token) {
      navigate('/')
      return
    }

    const check = () => {
      getClient()
        .ping()
        .then((resp) => {
          if (!resp.username) {
            setSessionExpired()
            clearToken()
            navigate('/')
          }
        })
        .catch((err) => {
          if (err instanceof ApiError && err.status === 401) {
            setSessionExpired()
            clearToken()
            navigate('/')
          }
        })
    }

    check()
    const id = setInterval(check, 30000)
    return () => clearInterval(id)
  }, [navigate])

  return getAccount()
}
