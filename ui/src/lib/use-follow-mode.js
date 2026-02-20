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
  const savedRef = useRef(null)

  useEffect(() => {
    if (isSearchActive) {
      if (savedRef.current === null) {
        savedRef.current = followMode
      }
      setFollowMode(false)
    } else if (savedRef.current !== null) {
      setFollowMode(savedRef.current)
      savedRef.current = null
    }
  }, [isSearchActive])

  const toggleFollow = useCallback(() => {
    setFollowMode((v) => !v)
  }, [])

  return [followMode, setFollowMode, toggleFollow]
}
