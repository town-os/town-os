import { useEffect, useRef } from 'react'

/**
 * Debounced journal search hook. Calls loadEntries when searchQuery,
 * sinceTime, or untilTime change, using a 1 000 ms debounce for time
 * changes and 300 ms for text-only changes. Skips the initial render
 * and resets when the journal unit is cleared.
 *
 * @param {string|null} journalUnit
 * @param {string} searchQuery
 * @param {string} sinceTime - value like YYYY-MM-DDTHH:00
 * @param {string} untilTime - value like YYYY-MM-DDTHH:00
 * @param {(unit: string, cursor: undefined, grep: string, since: number|undefined, until: number|undefined) => void} loadEntries
 */
export function useJournalSearch(journalUnit, searchQuery, sinceTime, untilTime, loadEntries) {
  const initRef = useRef(true)
  const prevSinceRef = useRef(sinceTime)
  const prevUntilRef = useRef(untilTime)

  useEffect(() => {
    if (!journalUnit) {
      initRef.current = true
      prevSinceRef.current = ''
      prevUntilRef.current = ''
      return
    }
    if (initRef.current) {
      initRef.current = false
      prevSinceRef.current = sinceTime
      prevUntilRef.current = untilTime
      return
    }
    const timeChanged =
      sinceTime !== prevSinceRef.current ||
      untilTime !== prevUntilRef.current
    prevSinceRef.current = sinceTime
    prevUntilRef.current = untilTime
    const delay = timeChanged ? 1000 : 300

    const sinceUnix = sinceTime
      ? Math.floor(new Date(sinceTime).getTime() / 1000)
      : undefined
    const untilUnix = untilTime
      ? Math.floor(new Date(untilTime).getTime() / 1000)
      : undefined
    const timer = setTimeout(() => {
      loadEntries(journalUnit, undefined, searchQuery, sinceUnix, untilUnix)
    }, delay)
    return () => clearTimeout(timer)
  }, [searchQuery, sinceTime, untilTime, journalUnit, loadEntries])
}
