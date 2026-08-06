import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

const navigate = vi.fn()
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}))

const mockPing = vi.fn()
vi.mock('./client-instance.js', () => ({
  default: () => ({ ping: mockPing }),
}))

import { useRequireAuth } from './hooks.js'
import { suspendSessionChecks, resetSessionChecks } from './session-guard.js'
import { setToken, getToken } from './auth.js'

describe('useRequireAuth', () => {
  beforeEach(() => {
    resetSessionChecks()
    navigate.mockClear()
    mockPing.mockReset()
    localStorage.clear()
    setToken('token-from-the-old-controller')
  })

  afterEach(() => {
    // Wrapped in act: resetSessionChecks notifies the suspension store's
    // subscribers, and useRequireAuth reads it through useSyncExternalStore —
    // so for any test that ends while still holding a suspension, this drops
    // it and re-renders a hook that is still mounted. Vitest runs this file's
    // afterEach before the testing-library cleanup that would have unmounted
    // it, so the update lands on a live component and React warns.
    act(() => {
      resetSessionChecks()
    })
  })

  it('logs out and navigates to login when the ping reports no session', async () => {
    // What a restarted controller answers a stale token: 200, but nobody
    // is logged in — the sessions table was cleared and the signing key
    // regenerated.
    mockPing.mockResolvedValue({ status: 'ok' })

    renderHook(() => useRequireAuth())

    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/'))
    expect(getToken()).toBeNull()
  })

  it('does not ping at all while session checks are suspended', async () => {
    suspendSessionChecks()
    mockPing.mockResolvedValue({ status: 'ok' })

    renderHook(() => useRequireAuth())

    await act(async () => {})
    // This is the whole point: during a Refresh Core Services the controller
    // is down, then comes back with every session invalidated. A poll running
    // across that would navigate away and unmount the dialog showing the
    // restart.
    expect(mockPing).not.toHaveBeenCalled()
    expect(navigate).not.toHaveBeenCalled()
    expect(getToken()).toBe('token-from-the-old-controller')
  })

  it('ignores a ping that resolves after a suspension is taken', async () => {
    let resolvePing
    mockPing.mockImplementation(() => new Promise((r) => { resolvePing = r }))

    renderHook(() => useRequireAuth())
    await waitFor(() => expect(mockPing).toHaveBeenCalled())

    // The refresh flow suspends while the first ping is still in flight.
    act(() => { suspendSessionChecks() })
    await act(async () => { resolvePing({ status: 'ok' }) })

    expect(navigate).not.toHaveBeenCalled()
    expect(getToken()).toBe('token-from-the-old-controller')
  })

  it('resumes checking once the suspension is released', async () => {
    const release = suspendSessionChecks()
    mockPing.mockResolvedValue({ status: 'ok' })

    renderHook(() => useRequireAuth())
    await act(async () => {})
    expect(navigate).not.toHaveBeenCalled()

    act(() => { release() })

    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/'))
  })

  it('navigates immediately when there is no token, suspended or not', async () => {
    localStorage.clear()
    suspendSessionChecks()

    renderHook(() => useRequireAuth())

    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/'))
    expect(mockPing).not.toHaveBeenCalled()
  })
})
