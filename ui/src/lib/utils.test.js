import { describe, it, expect, vi, afterEach } from 'vitest'
import { copyToClipboard, formatBytes, PAGE_SIZE } from './utils.js'

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
