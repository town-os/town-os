import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  isSessionCheckSuspended,
  suspendSessionChecks,
  subscribeSessionChecks,
  resetSessionChecks,
} from './session-guard.js'

describe('session-guard', () => {
  beforeEach(() => {
    resetSessionChecks()
  })

  it('is not suspended by default', () => {
    expect(isSessionCheckSuspended()).toBe(false)
  })

  it('suspends until the release function runs', () => {
    const release = suspendSessionChecks()
    expect(isSessionCheckSuspended()).toBe(true)
    release()
    expect(isSessionCheckSuspended()).toBe(false)
  })

  it('releases only once, so an unmount cleanup cannot drop someone else suspension', () => {
    const releaseA = suspendSessionChecks()
    const releaseB = suspendSessionChecks()

    releaseA()
    releaseA() // idempotent — must not cancel B's suspension
    expect(isSessionCheckSuspended()).toBe(true)

    releaseB()
    expect(isSessionCheckSuspended()).toBe(false)
  })

  it('notifies subscribers on suspend and release', () => {
    const listener = vi.fn()
    const unsubscribe = subscribeSessionChecks(listener)

    const release = suspendSessionChecks()
    expect(listener).toHaveBeenCalledTimes(1)
    release()
    expect(listener).toHaveBeenCalledTimes(2)

    unsubscribe()
    suspendSessionChecks()
    expect(listener).toHaveBeenCalledTimes(2)
  })
})
