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

  it('is sorted alphabetically after UTC', () => {
    const rest = TIMEZONES.slice(1)
    const sorted = [...rest].sort()
    expect(rest).toEqual(sorted)
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

  it('returns a valid offset for every entry in the TIMEZONES list', () => {
    for (const tz of TIMEZONES) {
      const offset = getTimezoneOffsetMinutes(tz)
      expect(typeof offset).toBe('number')
      expect(offset).toBeGreaterThanOrEqual(-720)
      expect(offset).toBeLessThanOrEqual(840)
    }
  })

  it('returns an offset that is a multiple of 15 for every timezone', () => {
    for (const tz of TIMEZONES) {
      const offset = getTimezoneOffsetMinutes(tz)
      expect(Math.abs(offset) % 15).toBe(0)
    }
  })
})
