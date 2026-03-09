import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useJournalSearch } from './use-journal-search.js'

describe('useJournalSearch', () => {
  /** @type {ReturnType<typeof vi.fn>} */
  let loadEntries

  beforeEach(() => {
    vi.useFakeTimers()
    loadEntries = vi.fn()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('skips the initial render and does not call loadEntries', () => {
    renderHook(() => useJournalSearch('nginx.service', '', '', '', loadEntries))
    act(() => { vi.advanceTimersByTime(2000) })
    expect(loadEntries).not.toHaveBeenCalled()
  })

  it('does not call loadEntries when journalUnit is null', () => {
    renderHook(() => useJournalSearch(null, 'query', '', '', loadEntries))
    act(() => { vi.advanceTimersByTime(2000) })
    expect(loadEntries).not.toHaveBeenCalled()
  })

  it('calls loadEntries after 300ms when searchQuery changes', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: 'error', since: '', until: '' })

    act(() => { vi.advanceTimersByTime(299) })
    expect(loadEntries).not.toHaveBeenCalled()

    act(() => { vi.advanceTimersByTime(1) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, 'error', undefined, undefined, undefined)
  })

  it('calls loadEntries after 300ms when sinceTime changes', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: '', since: '2026-02-19T14:00', until: '' })

    act(() => { vi.advanceTimersByTime(299) })
    expect(loadEntries).not.toHaveBeenCalled()

    act(() => { vi.advanceTimersByTime(1) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    const sinceUnix = Math.floor(new Date('2026-02-19T14:00').getTime() / 1000)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', sinceUnix, undefined, undefined)
  })

  it('calls loadEntries after 300ms when untilTime changes', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: '', since: '', until: '2026-02-19T15:00' })

    act(() => { vi.advanceTimersByTime(299) })
    expect(loadEntries).not.toHaveBeenCalled()

    act(() => { vi.advanceTimersByTime(1) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    const untilUnix = Math.floor(new Date('2026-02-19T15:00').getTime() / 1000)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', undefined, untilUnix, undefined)
  })

  it('passes both sinceUnix and untilUnix when both are set', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: '', since: '2026-02-19T10:00', until: '2026-02-19T14:00' })

    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    const sinceUnix = Math.floor(new Date('2026-02-19T10:00').getTime() / 1000)
    const untilUnix = Math.floor(new Date('2026-02-19T14:00').getTime() / 1000)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', sinceUnix, untilUnix, undefined)
  })

  it('debounces rapid sinceTime changes and only fires once', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: '', since: '2026-02-19T13:00', until: '' })
    act(() => { vi.advanceTimersByTime(100) })
    rerender({ unit: 'nginx.service', query: '', since: '2026-02-19T14:00', until: '' })
    act(() => { vi.advanceTimersByTime(100) })
    rerender({ unit: 'nginx.service', query: '', since: '2026-02-19T15:00', until: '' })
    act(() => { vi.advanceTimersByTime(300) })

    expect(loadEntries).toHaveBeenCalledTimes(1)
    const sinceUnix = Math.floor(new Date('2026-02-19T15:00').getTime() / 1000)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', sinceUnix, undefined, undefined)
  })

  it('converts sinceTime to unix seconds', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: '', since: '2026-01-15T08:00', until: '' })
    act(() => { vi.advanceTimersByTime(300) })

    const expected = Math.floor(new Date('2026-01-15T08:00').getTime() / 1000)
    expect(loadEntries.mock.calls[0][3]).toBe(expected)
    expect(expected).toBeGreaterThan(0)
  })

  it('passes undefined for since and until when both are empty', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: 'test', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(300) })

    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, 'test', undefined, undefined, undefined)
  })

  it('resets on journalUnit change to null and skips next initial render', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    // Switch to null (close journal).
    rerender({ unit: null, query: '', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(2000) })
    expect(loadEntries).not.toHaveBeenCalled()

    // Re-open: first render after null should be skipped (init guard).
    rerender({ unit: 'redis.service', query: '', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(2000) })
    expect(loadEntries).not.toHaveBeenCalled()

    // Now a real change should work.
    rerender({ unit: 'redis.service', query: 'started', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('redis.service', undefined, 'started', undefined, undefined, undefined)
  })

  it('clears sinceTime by setting it back to empty and triggers search', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    // Set since time.
    rerender({ unit: 'nginx.service', query: '', since: '2026-02-19T14:00', until: '' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    loadEntries.mockClear()

    // Clear since time.
    rerender({ unit: 'nginx.service', query: '', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', undefined, undefined, undefined)
  })

  it('clears searchQuery and triggers search', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    // Set search query.
    rerender({ unit: 'nginx.service', query: 'error', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    loadEntries.mockClear()

    // Clear search query.
    rerender({ unit: 'nginx.service', query: '', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', undefined, undefined, undefined)
  })

  it('clears untilTime by setting it back to empty and triggers search', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    // Set until time.
    rerender({ unit: 'nginx.service', query: '', since: '', until: '2026-02-19T16:00' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    loadEntries.mockClear()

    // Clear until time.
    rerender({ unit: 'nginx.service', query: '', since: '', until: '' })
    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', undefined, undefined, undefined)
  })

  it('passes priority when set', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until, priority }) => useJournalSearch(unit, query, since, until, loadEntries, priority),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '', priority: 3 } },
    )

    rerender({ unit: 'nginx.service', query: 'error', since: '', until: '', priority: 3 })

    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, 'error', undefined, undefined, 3)
  })

  it('passes undefined priority when priority is 0', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until, priority }) => useJournalSearch(unit, query, since, until, loadEntries, priority),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '', priority: 0 } },
    )

    rerender({ unit: 'nginx.service', query: 'test', since: '', until: '', priority: 0 })

    act(() => { vi.advanceTimersByTime(300) })
    expect(loadEntries).toHaveBeenCalledTimes(1)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, 'test', undefined, undefined, undefined)
  })

  it('debounces rapid untilTime changes and only fires once', () => {
    const { rerender } = renderHook(
      ({ unit, query, since, until }) => useJournalSearch(unit, query, since, until, loadEntries),
      { initialProps: { unit: 'nginx.service', query: '', since: '', until: '' } },
    )

    rerender({ unit: 'nginx.service', query: '', since: '', until: '2026-02-19T13:00' })
    act(() => { vi.advanceTimersByTime(100) })
    rerender({ unit: 'nginx.service', query: '', since: '', until: '2026-02-19T14:00' })
    act(() => { vi.advanceTimersByTime(100) })
    rerender({ unit: 'nginx.service', query: '', since: '', until: '2026-02-19T15:00' })
    act(() => { vi.advanceTimersByTime(300) })

    expect(loadEntries).toHaveBeenCalledTimes(1)
    const untilUnix = Math.floor(new Date('2026-02-19T15:00').getTime() / 1000)
    expect(loadEntries).toHaveBeenCalledWith('nginx.service', undefined, '', undefined, untilUnix, undefined)
  })
})
