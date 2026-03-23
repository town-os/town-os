import { useState, useEffect, useCallback, useRef } from 'react'

/**
 * Hook that manages follow mode for journal tailing. Automatically
 * disables follow when a search or time filter is active, and restores
 * the previous follow state when filters are cleared.
 *
 * @param {boolean} isSearchActive - true when any search or time filter is set
 * @returns {[boolean, (v: boolean) => void, () => void]}
 */
export function useFollowMode(isSearchActive) {
  const [followMode, setFollowMode] = useState(true)
  const followModeRef = useRef(followMode)
  const savedRef = useRef(null)

  useEffect(() => {
    followModeRef.current = followMode
  }, [followMode])

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (isSearchActive) {
      if (savedRef.current === null) {
        savedRef.current = followModeRef.current
      }
      setFollowMode(false)
    } else if (savedRef.current !== null) {
      setFollowMode(savedRef.current)
      savedRef.current = null
    }
  }, [isSearchActive])
  /* eslint-enable react-hooks/set-state-in-effect */

  const toggleFollow = useCallback(() => {
    setFollowMode((v) => !v)
  }, [])

  return [followMode, setFollowMode, toggleFollow]
}
