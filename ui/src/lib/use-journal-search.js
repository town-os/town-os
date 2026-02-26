import { useEffect, useRef } from 'react'

/**
 * Debounced journal search hook. Calls loadEntries when searchQuery,
 * sinceTime, or untilTime change, using a 300 ms debounce. Skips the
 * initial render and resets when the journal unit is cleared.
 *
 * @param {string|null} journalUnit
 * @param {string} searchQuery
 * @param {string} sinceTime - value like YYYY-MM-DDTHH:00
 * @param {string} untilTime - value like YYYY-MM-DDTHH:00
 * @param {(unit: string, cursor: undefined, grep: string, since: number|undefined, until: number|undefined, priority: number|undefined) => void} loadEntries
 * @param {number} [priority=0] - Max priority level (0=no filter)
 */
export function useJournalSearch(journalUnit, searchQuery, sinceTime, untilTime, loadEntries, priority = 0) {
  const initRef = useRef(true)

  useEffect(() => {
    if (!journalUnit) {
      initRef.current = true
      return
    }
    if (initRef.current) {
      initRef.current = false
      return
    }

    const sinceUnix = sinceTime
      ? Math.floor(new Date(sinceTime).getTime() / 1000)
      : undefined
    const untilUnix = untilTime
      ? Math.floor(new Date(untilTime).getTime() / 1000)
      : undefined
    const timer = setTimeout(() => {
      loadEntries(journalUnit, undefined, searchQuery, sinceUnix, untilUnix, priority || undefined)
    }, 300)
    return () => clearTimeout(timer)
  }, [searchQuery, sinceTime, untilTime, journalUnit, loadEntries, priority])
}
