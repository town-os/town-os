import { describe, it, expect } from 'vitest'
import { formatBytes, PAGE_SIZE } from './utils.js'

describe('PAGE_SIZE', () => {
  it('is 20', () => {
    expect(PAGE_SIZE).toBe(20)
  })
})

describe('formatBytes', () => {
  it('returns 0 B for zero', () => {
    expect(formatBytes(0)).toBe('0 B')
  })

  it('formats bytes', () => {
    expect(formatBytes(500)).toBe('500 B')
  })

  it('formats kilobytes', () => {
    expect(formatBytes(1024)).toBe('1.0 KB')
  })

  it('formats megabytes', () => {
    expect(formatBytes(1048576)).toBe('1.0 MB')
  })

  it('formats gigabytes', () => {
    expect(formatBytes(1073741824)).toBe('1.0 GB')
  })

  it('formats terabytes', () => {
    expect(formatBytes(1099511627776)).toBe('1.0 TB')
  })

  it('formats fractional values', () => {
    const result = formatBytes(1536)
    expect(result).toBe('1.5 KB')
  })

  it('rounds large values', () => {
    const result = formatBytes(50 * 1024 * 1024 * 1024)
    expect(result).toBe('50 GB')
  })
})
