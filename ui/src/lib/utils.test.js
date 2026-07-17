import { describe, it, expect, vi, afterEach } from 'vitest'
import { copyToClipboard, formatBytes, formatAgo, PAGE_SIZE } from './utils.js'

describe('formatAgo', () => {
  const now = new Date('2026-07-16T12:00:00Z')

  it.each([
    ['seconds', '2026-07-16T11:59:30Z', '30s'],
    ['just now', '2026-07-16T12:00:00Z', '0s'],
    ['minutes', '2026-07-16T11:45:00Z', '15m'],
    ['the minute boundary', '2026-07-16T11:59:00Z', '1m'],
    ['hours', '2026-07-16T09:00:00Z', '3h'],
    ['the hour boundary', '2026-07-16T11:00:00Z', '1h'],
    ['days', '2026-07-14T12:00:00Z', '2d'],
    ['the day boundary', '2026-07-15T12:00:00Z', '1d'],
  ])('renders %s', (_label, iso, want) => {
    expect(formatAgo(iso, now)).toBe(want)
  })

  // A missing stamp means the event never happened. Returning a duration would
  // claim it did.
  it.each([[undefined], [null], ['']])('returns null for a missing timestamp (%s)', (v) => {
    expect(formatAgo(v, now)).toBeNull()
  })

  it('returns null for an unparseable timestamp', () => {
    expect(formatAgo('not-a-date', now)).toBeNull()
  })

  // Clock skew between the box and the browser must not render as a negative age.
  it('clamps a future timestamp to zero', () => {
    expect(formatAgo('2026-07-16T12:00:30Z', now)).toBe('0s')
  })
})

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

// A Town OS box is reached over plain HTTP on the LAN, so isSecureContext is
// false and navigator.clipboard is absent: the execCommand fallback is the path
// nearly every real copy takes. These pin its behavior, because a fallback that
// quietly copies the wrong thing still reports success to the caller.
describe('copyToClipboard', () => {
  const origClipboard = navigator.clipboard
  const origSecure = window.isSecureContext

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: origClipboard, writable: true, configurable: true,
    })
    window.isSecureContext = origSecure
    delete document.execCommand
    document.body.innerHTML = ''
  })

  function insecureContext(execCommand) {
    Object.defineProperty(navigator, 'clipboard', {
      value: undefined, writable: true, configurable: true,
    })
    window.isSecureContext = false
    document.execCommand = execCommand
  }

  it('uses the async clipboard API in a secure context', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText }, writable: true, configurable: true,
    })
    window.isSecureContext = true

    await copyToClipboard('s3cret')

    expect(writeText).toHaveBeenCalledWith('s3cret')
  })

  it('copies the exact text via the fallback when the context is insecure', async () => {
    let copied
    insecureContext(vi.fn(() => {
      // What execCommand('copy') would actually put on the clipboard: the
      // selected text of the focused element.
      copied = document.activeElement?.value
      return true
    }))

    await copyToClipboard('qw7m3T2jDytu7je3SMf0aBn9DYHC3sVc')

    expect(copied).toBe('qw7m3T2jDytu7je3SMf0aBn9DYHC3sVc')
  })

  // The bug this guards: mounted on document.body, the scratch textarea sits
  // outside a modal dialog's focus scope. The dialog pulls focus back, the
  // selection is dropped, and execCommand copies the wrong text while still
  // returning true -- so the user pastes a stale value and is told it worked.
  it('mounts the scratch textarea inside the container it is given', async () => {
    const dialog = document.createElement('div')
    document.body.appendChild(dialog)

    let parentDuringCopy
    insecureContext(vi.fn(() => {
      parentDuringCopy = document.activeElement?.parentElement
      return true
    }))

    await copyToClipboard('token', dialog)

    expect(parentDuringCopy).toBe(dialog)
  })

  it('selects the whole value, so the copy cannot be truncated', async () => {
    let selection
    insecureContext(vi.fn(() => {
      const el = document.activeElement
      selection = el?.value?.slice(el.selectionStart, el.selectionEnd)
      return true
    }))

    await copyToClipboard('0123456789abcdef')

    expect(selection).toBe('0123456789abcdef')
  })

  it('removes the textarea and restores focus once the copy is done', async () => {
    const dialog = document.createElement('div')
    const button = document.createElement('button')
    dialog.appendChild(button)
    document.body.appendChild(dialog)
    button.focus()

    insecureContext(vi.fn(() => true))

    await copyToClipboard('token', dialog)

    expect(dialog.querySelector('textarea')).toBeNull()
    expect(document.activeElement).toBe(button)
  })

  it('rejects when the copy fails rather than reporting success', async () => {
    insecureContext(vi.fn(() => false))

    await expect(copyToClipboard('token')).rejects.toThrow()
    // Even on the failure path the scratch element must not be left behind.
    expect(document.querySelector('textarea')).toBeNull()
  })
})
