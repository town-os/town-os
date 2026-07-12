import { useState, useEffect, useCallback, useRef, useSyncExternalStore } from 'react'
import { useNavigate } from 'react-router-dom'
import getClient from './client-instance.js'
import { getToken, clearToken, getAccount, setSessionExpired } from './auth.js'
import { isSessionCheckSuspended, subscribeSessionChecks } from './session-guard.js'
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

  const fetcherRef = useRef(fetcher)
  useEffect(() => {
    fetcherRef.current = fetcher
  })

  const depsKey = JSON.stringify(deps)

  const refresh = useCallback(() => {
    const gen = ++generationRef.current
    setLoading(true)
    fetcherRef.current()
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [depsKey])

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, interval)
    return () => clearInterval(id)
  }, [refresh, interval])

  return [data, refresh, loading]
}

/**
 * Hook that reports whether session-validity polling is currently suspended.
 * @returns {boolean}
 */
export function useSessionChecksSuspended() {
  return useSyncExternalStore(subscribeSessionChecks, isSessionCheckSuspended)
}

/**
 * Hook that verifies the session is still valid, redirecting to login if not.
 *
 * The poll stands down while a caller holds a suspension (see
 * session-guard.js). A Refresh Core Services restart invalidates every
 * session — new signing key, sessions table cleared — so a poll running
 * across it would bounce the operator to the login page the instant the
 * successor answered, tearing down the dialog showing them the restart.
 */
export function useRequireAuth() {
  const navigate = useNavigate()
  const suspended = useSessionChecksSuspended()

  useEffect(() => {
    const token = getToken()
    if (!token) {
      navigate('/')
      return
    }
    if (suspended) return

    const logout = () => {
      // A suspension taken while this ping was in flight still counts:
      // the answer describes a controller the caller already told us not
      // to judge the session by.
      if (isSessionCheckSuspended()) return
      setSessionExpired()
      clearToken()
      navigate('/')
    }

    const check = () => {
      getClient()
        .ping()
        .then((resp) => {
          if (!resp.username) logout()
        })
        .catch((err) => {
          if (err instanceof ApiError && err.status === 401) logout()
        })
    }

    check()
    const id = setInterval(check, 60000)
    return () => clearInterval(id)
  }, [navigate, suspended])

  return getAccount()
}
