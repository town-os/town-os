import { describe, it, expect } from 'vitest'
import { TIMEZONES, getTimezoneOffsetMinutes } from './timezones'

describe('TIMEZONES', () => {
  it('contains UTC', () => {
    expect(TIMEZONES).toContain('UTC')
  })

  it('is non-empty', () => {
    expect(TIMEZONES.length).toBeGreaterThan(0)
  })

  it('starts with UTC', () => {
    expect(TIMEZONES[0]).toBe('UTC')
  })

  it('contains major timezone regions', () => {
    expect(TIMEZONES).toContain('America/New_York')
    expect(TIMEZONES).toContain('Europe/London')
    expect(TIMEZONES).toContain('Asia/Tokyo')
    expect(TIMEZONES).toContain('Pacific/Auckland')
    expect(TIMEZONES).toContain('Australia/Sydney')
    expect(TIMEZONES).toContain('Africa/Cairo')
  })

  it('contains only unique entries', () => {
    const unique = new Set(TIMEZONES)
    expect(unique.size).toBe(TIMEZONES.length)
  })
})

describe('getTimezoneOffsetMinutes', () => {
  it('returns 0 for UTC', () => {
    expect(getTimezoneOffsetMinutes('UTC')).toBe(0)
  })

  it('returns a number for a valid timezone', () => {
    const offset = getTimezoneOffsetMinutes('America/New_York')
    expect(typeof offset).toBe('number')
    // New York is UTC-5 or UTC-4 depending on DST
    expect(offset).toBeGreaterThanOrEqual(-300)
    expect(offset).toBeLessThanOrEqual(-240)
  })

  it('returns 540 for Asia/Tokyo (no DST)', () => {
    expect(getTimezoneOffsetMinutes('Asia/Tokyo')).toBe(540)
  })

  it('returns -600 for Pacific/Honolulu (no DST)', () => {
    expect(getTimezoneOffsetMinutes('Pacific/Honolulu')).toBe(-600)
  })

  it('returns 330 for Asia/Kolkata (no DST)', () => {
    expect(getTimezoneOffsetMinutes('Asia/Kolkata')).toBe(330)
  })
})
