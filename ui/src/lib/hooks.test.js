import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { usePolling } from './hooks.js'

// Mock react-router-dom to prevent import errors from useRequireAuth.
vi.mock('react-router-dom', () => ({
  useNavigate: () => vi.fn(),
}))

describe('usePolling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts with loading=true', () => {
    const fetcher = vi.fn(() => new Promise(() => {})) // never resolves
    const { result } = renderHook(() => usePolling(fetcher, null, [], 5000))
    expect(result.current[2]).toBe(true) // loading
  })

  it('sets loading=false after fetcher resolves', async () => {
    let resolve
    const fetcher = vi.fn(() => new Promise((r) => { resolve = r }))
    const { result } = renderHook(() => usePolling(fetcher, null, [], 5000))
    expect(result.current[2]).toBe(true)

    await act(async () => {
      resolve({ data: 'test' })
    })

    expect(result.current[2]).toBe(false)
    expect(result.current[0]).toEqual({ data: 'test' })
  })

  it('sets loading=false after fetcher rejects', async () => {
    let reject
    const fetcher = vi.fn(() => new Promise((_, r) => { reject = r }))
    const { result } = renderHook(() => usePolling(fetcher, 'default', [], 5000))
    expect(result.current[2]).toBe(true)

    await act(async () => {
      reject(new Error('fail'))
    })

    expect(result.current[2]).toBe(false)
    expect(result.current[0]).toBe('default')
  })

  it('returns data, refresh, and loading', async () => {
    const fetcher = vi.fn(() => Promise.resolve([1, 2, 3]))
    const { result } = renderHook(() => usePolling(fetcher, [], [], 5000))

    await act(async () => {})

    const [data, refresh, loading] = result.current
    expect(data).toEqual([1, 2, 3])
    expect(typeof refresh).toBe('function')
    expect(loading).toBe(false)
  })

  it('discards stale responses when deps change', async () => {
    // First fetcher: slow, resolves to 'stale'
    let resolveStale
    const staleFetcher = vi.fn(() => new Promise((r) => { resolveStale = r }))

    // Second fetcher: fast, resolves to 'fresh'
    const freshFetcher = vi.fn(() => Promise.resolve('fresh'))

    const { result, rerender } = renderHook(
      ({ fetcher, dep }) => usePolling(fetcher, 'default', [dep], 60000),
      { initialProps: { fetcher: staleFetcher, dep: 1 } },
    )

    // The stale fetcher is in flight but hasn't resolved yet
    expect(result.current[2]).toBe(true)

    // Change deps — triggers a new generation with the fresh fetcher
    rerender({ fetcher: freshFetcher, dep: 2 })

    // Fresh fetcher resolves immediately
    await act(async () => {})
    expect(result.current[0]).toBe('fresh')
    expect(result.current[2]).toBe(false)

    // Now the stale response comes back — it should be ignored
    await act(async () => {
      resolveStale('stale')
    })

    // Data should still be 'fresh', not overwritten by 'stale'
    expect(result.current[0]).toBe('fresh')
  })
})
