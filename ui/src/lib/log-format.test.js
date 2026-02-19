import { describe, it, expect } from 'vitest'
import { parseAnsi, parseFields, stripAnsi, groupByMinute, ANSI_GREYS } from './log-format.js'

describe('parseAnsi', () => {
  it('returns single segment for plain text', () => {
    const result = parseAnsi('hello world')
    expect(result).toEqual([{ str: 'hello world', color: null, bold: false }])
  })

  it('returns empty array for empty string', () => {
    expect(parseAnsi('')).toEqual([])
  })

  it('parses single color code', () => {
    const result = parseAnsi('\x1b[31mred text\x1b[0m')
    expect(result).toEqual([
      { str: 'red text', color: ANSI_GREYS['31'], bold: false },
    ])
  })

  it('parses bold code', () => {
    const result = parseAnsi('\x1b[1mbold text\x1b[0m')
    expect(result).toEqual([
      { str: 'bold text', color: null, bold: true },
    ])
  })

  it('parses combined bold and color', () => {
    const result = parseAnsi('\x1b[1;31mbold red\x1b[0m')
    expect(result).toEqual([
      { str: 'bold red', color: ANSI_GREYS['31'], bold: true },
    ])
  })

  it('handles reset code', () => {
    const result = parseAnsi('\x1b[31mred\x1b[0m plain')
    expect(result).toEqual([
      { str: 'red', color: ANSI_GREYS['31'], bold: false },
      { str: ' plain', color: null, bold: false },
    ])
  })

  it('handles text before first code', () => {
    const result = parseAnsi('before \x1b[32mgreen')
    expect(result).toEqual([
      { str: 'before ', color: null, bold: false },
      { str: 'green', color: ANSI_GREYS['32'], bold: false },
    ])
  })

  it('handles multiple color switches', () => {
    const result = parseAnsi('\x1b[31mred\x1b[32mgreen\x1b[0mplain')
    expect(result).toEqual([
      { str: 'red', color: ANSI_GREYS['31'], bold: false },
      { str: 'green', color: ANSI_GREYS['32'], bold: false },
      { str: 'plain', color: null, bold: false },
    ])
  })

  it('maps all standard ANSI color codes to greyscale', () => {
    for (const code of Object.keys(ANSI_GREYS)) {
      const result = parseAnsi(`\x1b[${code}mtext`)
      expect(result[0].color).toBe(ANSI_GREYS[code])
    }
  })
})

describe('parseFields', () => {
  it('returns single text segment for plain text', () => {
    expect(parseFields('hello world')).toEqual([
      { type: 'text', value: 'hello world' },
    ])
  })

  it('returns empty array for empty string', () => {
    expect(parseFields('')).toEqual([])
  })

  it('parses single name=value field', () => {
    expect(parseFields('key=value')).toEqual([
      { type: 'field', name: 'key', eq: '=', value: 'value' },
    ])
  })

  it('parses field with surrounding text', () => {
    expect(parseFields('before key=value after')).toEqual([
      { type: 'text', value: 'before ' },
      { type: 'field', name: 'key', eq: '=', value: 'value' },
      { type: 'text', value: ' after' },
    ])
  })

  it('parses multiple fields', () => {
    const result = parseFields('host=localhost port=8080')
    expect(result).toEqual([
      { type: 'field', name: 'host', eq: '=', value: 'localhost' },
      { type: 'text', value: ' ' },
      { type: 'field', name: 'port', eq: '=', value: '8080' },
    ])
  })

  it('handles dotted field names', () => {
    expect(parseFields('server.name=foo')).toEqual([
      { type: 'field', name: 'server.name', eq: '=', value: 'foo' },
    ])
  })

  it('handles underscore field names', () => {
    expect(parseFields('my_var=123')).toEqual([
      { type: 'field', name: 'my_var', eq: '=', value: '123' },
    ])
  })

  it('handles empty value', () => {
    const result = parseFields('key= next')
    expect(result).toEqual([
      { type: 'field', name: 'key', eq: '=', value: '' },
      { type: 'text', value: ' next' },
    ])
  })
})

describe('stripAnsi', () => {
  it('returns plain text unchanged', () => {
    expect(stripAnsi('hello world')).toBe('hello world')
  })

  it('strips single color code', () => {
    expect(stripAnsi('\x1b[31mred text\x1b[0m')).toBe('red text')
  })

  it('strips multiple codes', () => {
    expect(stripAnsi('\x1b[1;31mbold red\x1b[0m and \x1b[32mgreen\x1b[0m')).toBe('bold red and green')
  })

  it('strips codes with no text between', () => {
    expect(stripAnsi('\x1b[0m\x1b[31m\x1b[0m')).toBe('')
  })

  it('handles empty string', () => {
    expect(stripAnsi('')).toBe('')
  })
})

describe('groupByMinute', () => {
  it('returns empty array for empty input', () => {
    expect(groupByMinute([])).toEqual([])
  })

  it('groups entries in the same minute together', () => {
    const entries = [
      { RealtimeTimestamp: '2025-01-01T10:05:00Z', Message: 'a' },
      { RealtimeTimestamp: '2025-01-01T10:05:30Z', Message: 'b' },
      { RealtimeTimestamp: '2025-01-01T10:05:59Z', Message: 'c' },
    ]
    const groups = groupByMinute(entries)
    expect(groups).toHaveLength(1)
    expect(groups[0].entries).toHaveLength(3)
  })

  it('separates entries in different minutes', () => {
    const entries = [
      { RealtimeTimestamp: '2025-01-01T10:05:00Z', Message: 'a' },
      { RealtimeTimestamp: '2025-01-01T10:06:00Z', Message: 'b' },
      { RealtimeTimestamp: '2025-01-01T10:07:00Z', Message: 'c' },
    ]
    const groups = groupByMinute(entries)
    expect(groups).toHaveLength(3)
    expect(groups[0].entries).toHaveLength(1)
    expect(groups[1].entries).toHaveLength(1)
    expect(groups[2].entries).toHaveLength(1)
  })

  it('creates groups with unique keys', () => {
    const entries = [
      { RealtimeTimestamp: '2025-01-01T10:05:00Z', Message: 'a' },
      { RealtimeTimestamp: '2025-01-01T10:06:00Z', Message: 'b' },
    ]
    const groups = groupByMinute(entries)
    const keys = groups.map((g) => g.key)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('groups have label property', () => {
    const entries = [
      { RealtimeTimestamp: '2025-01-01T10:05:00Z', Message: 'a' },
    ]
    const groups = groupByMinute(entries)
    expect(groups[0].label).toBeTruthy()
    expect(typeof groups[0].label).toBe('string')
  })

  it('handles mixed minutes correctly', () => {
    const entries = [
      { RealtimeTimestamp: '2025-01-01T10:05:00Z', Message: 'a' },
      { RealtimeTimestamp: '2025-01-01T10:05:30Z', Message: 'b' },
      { RealtimeTimestamp: '2025-01-01T10:06:00Z', Message: 'c' },
      { RealtimeTimestamp: '2025-01-01T10:06:45Z', Message: 'd' },
      { RealtimeTimestamp: '2025-01-01T10:07:00Z', Message: 'e' },
    ]
    const groups = groupByMinute(entries)
    expect(groups).toHaveLength(3)
    expect(groups[0].entries).toHaveLength(2)
    expect(groups[1].entries).toHaveLength(2)
    expect(groups[2].entries).toHaveLength(1)
  })
})
