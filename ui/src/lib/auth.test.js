import { describe, it, expect, beforeEach } from 'vitest'
import {
  setSessionExpired,
  getAndClearSessionExpired,
} from './auth.js'

describe('session expired flag', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('returns false when no flag is set', () => {
    expect(getAndClearSessionExpired()).toBe(false)
  })

  it('returns true after setSessionExpired and clears the flag', () => {
    setSessionExpired()
    expect(getAndClearSessionExpired()).toBe(true)
    // Second call should return false (flag cleared)
    expect(getAndClearSessionExpired()).toBe(false)
  })
})
