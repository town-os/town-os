import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TooltipProvider } from '@/components/ui/tooltip'
import PackageInfoDialog from './PackageInfoDialog.jsx'

const toastSuccess = vi.fn()
const toastError = vi.fn()
vi.mock('sonner', () => ({
  toast: {
    success: (...args) => toastSuccess(...args),
    error: (...args) => toastError(...args),
  },
}))

// Radix's tooltip measures its trigger, which jsdom does not implement.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
})

function renderInfo(dialog) {
  render(
    <TooltipProvider>
      <PackageInfoDialog
        dialog={{ open: true, name: 'synapse', version: '1.0', ...dialog }}
        onClose={vi.fn()}
      />
    </TooltipProvider>,
  )
}

describe('PackageInfoDialog boolean answers', () => {
  // Boolean answers are stored as the strings "true"/"false"; showing those raw
  // in the configuration list reads like a bug next to a checkbox in the
  // install dialog.
  it('renders a true answer as Yes', () => {
    renderInfo({
      questions: { open: { query: 'Allow open registration?', type: 'boolean' } },
      responses: { open: 'true' },
    })
    expect(screen.getByText('Yes')).toBeTruthy()
    expect(screen.queryByText('true')).toBeNull()
  })

  it('renders a false answer as No', () => {
    renderInfo({
      questions: { open: { query: 'Allow open registration?', type: 'boolean' } },
      responses: { open: 'false' },
    })
    expect(screen.getByText('No')).toBeTruthy()
    expect(screen.queryByText('false')).toBeNull()
  })

  it('renders an unanswered boolean as a dash', () => {
    renderInfo({
      questions: { open: { query: 'Allow open registration?', type: 'boolean' } },
      responses: {},
    })
    expect(screen.getByText('-')).toBeTruthy()
  })

  it('leaves non-boolean answers untouched', () => {
    renderInfo({
      questions: {
        port: { query: 'Port?', type: 'port' },
        pass: { query: 'Password?', type: 'secret' },
      },
      responses: { port: '8080', pass: 'hunter2' },
    })
    expect(screen.getByText('8080')).toBeTruthy()
    // Secrets stay masked.
    expect(screen.getByText('********')).toBeTruthy()
    expect(screen.queryByText('hunter2')).toBeNull()
  })
})

describe('PackageInfoDialog secret answers', () => {
  const origClipboard = navigator.clipboard

  beforeEach(() => {
    toastSuccess.mockClear()
    toastError.mockClear()
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: origClipboard,
      writable: true,
      configurable: true,
    })
  })

  function renderSecret() {
    renderInfo({
      questions: { pass: { query: 'Password?', type: 'secret' } },
      responses: { pass: 'hunter2' },
    })
  }

  it('copies the unmasked secret to the clipboard and toasts on success', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    })
    window.isSecureContext = true

    renderSecret()
    await userEvent.click(screen.getByRole('button', { name: 'Copy secret to clipboard' }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('hunter2'))
    expect(toastSuccess).toHaveBeenCalledWith('Copied to clipboard')
    // The value itself never reaches the screen.
    expect(screen.queryByText('hunter2')).toBeNull()
    expect(screen.getByText('********')).toBeTruthy()
  })

  it('shows a copy affordance on the mask, and a check once copied', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    })
    window.isSecureContext = true

    renderSecret()
    const btn = screen.getByRole('button', { name: 'Copy secret to clipboard' })
    // A bare mask reads as inert text; the copy icon is what says "click me".
    expect(btn.querySelector('.lucide-copy')).toBeTruthy()

    await userEvent.click(btn)

    await waitFor(() => expect(btn.querySelector('.lucide-check')).toBeTruthy())
    expect(btn.querySelector('.lucide-copy')).toBeNull()
  })

  it('explains the click in a tooltip on hover', async () => {
    renderSecret()
    const btn = screen.getByRole('button', { name: 'Copy secret to clipboard' })

    fireEvent.focus(btn)

    await waitFor(() => {
      expect(screen.getAllByText('Click to copy to clipboard').length).toBeGreaterThan(0)
    })
  })

  it('falls back to execCommand outside a secure context', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: undefined,
      writable: true,
      configurable: true,
    })
    window.isSecureContext = false
    const execCommand = vi.fn().mockReturnValue(true)
    document.execCommand = execCommand

    renderSecret()
    await userEvent.click(screen.getByRole('button', { name: 'Copy secret to clipboard' }))

    await waitFor(() => expect(execCommand).toHaveBeenCalledWith('copy'))
    expect(toastSuccess).toHaveBeenCalledWith('Copied to clipboard')
  })

  // The regression: a Town OS box is served over plain HTTP, so this fallback is
  // the path every real copy takes. Asserting execCommand was *called* is not
  // enough -- it returns true, and the UI toasts "copied", even when the wrong
  // text (or none) reaches the clipboard. What matters is the text that would be
  // copied: the selection of the focused element at that moment.
  it('copies the real secret -- not an empty or stale selection -- outside a secure context', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: undefined,
      writable: true,
      configurable: true,
    })
    window.isSecureContext = false

    let copiedText
    let copiedFromParent
    document.execCommand = vi.fn(() => {
      const el = document.activeElement
      copiedText = el?.value?.slice(el.selectionStart, el.selectionEnd)
      // Read the parent here, while the copy is happening: the scratch element is
      // detached again as soon as copyToClipboard returns, and a detached node
      // reports a null parent.
      copiedFromParent = el?.parentElement
      return true
    })

    renderSecret()
    const btn = screen.getByRole('button', { name: 'Copy secret to clipboard' })
    await userEvent.click(btn)

    await waitFor(() => expect(copiedText).toBe('hunter2'))
    // And it must be selected from an element mounted alongside the button, i.e.
    // inside the dialog. On document.body it sits outside the dialog's focus
    // scope, which pulls focus back and drops the selection -- the copy then
    // silently yields whatever else was selected, and still reports success.
    expect(copiedFromParent).toBe(btn.parentElement)
    expect(copiedFromParent).not.toBe(document.body)
  })

  it('toasts an error when the copy fails', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    })
    window.isSecureContext = true

    renderSecret()
    await userEvent.click(screen.getByRole('button', { name: 'Copy secret to clipboard' }))

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('Could not copy to clipboard'))
    expect(toastSuccess).not.toHaveBeenCalled()
  })

  it('renders an unanswered secret as a plain dash with nothing to copy', () => {
    renderInfo({
      questions: { pass: { query: 'Password?', type: 'secret' } },
      responses: {},
    })
    expect(screen.getByText('-')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Copy secret to clipboard' })).toBeNull()
  })
})
