import { describe, it, expect } from 'vitest'
import { formatDetail } from './audit-format.js'

describe('formatDetail', () => {
  it('returns dash for empty string', () => {
    expect(formatDetail('')).toBe('-')
  })

  it('returns dash for null', () => {
    expect(formatDetail(null)).toBe('-')
  })

  it('returns dash for undefined', () => {
    expect(formatDetail(undefined)).toBe('-')
  })

  it('formats simple key-value pairs', () => {
    expect(formatDetail('{"name":"nginx","version":"1.0"}')).toBe(
      'name=nginx, version=1.0',
    )
  })

  it('formats nested objects as JSON', () => {
    expect(formatDetail('{"name":"nginx","responses":{"port":"8080"}}')).toBe(
      'name=nginx, responses={"port":"8080"}',
    )
  })

  it('formats boolean values', () => {
    expect(formatDetail('{"admin":true}')).toBe('admin=true')
  })

  it('formats numeric values', () => {
    expect(formatDetail('{"quota":1024}')).toBe('quota=1024')
  })

  it('returns raw string for invalid JSON', () => {
    expect(formatDetail('not json')).toBe('not json')
  })

  it('formats single field', () => {
    expect(formatDetail('{"username":"admin"}')).toBe('username=admin')
  })

  it('formats multiple fields with comma separator', () => {
    const result = formatDetail('{"name":"data","action":"start"}')
    expect(result).toBe('name=data, action=start')
  })
})
