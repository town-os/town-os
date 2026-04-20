import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, act, cleanup } from '@testing-library/react'
import { I18nProvider } from '@/i18n/I18nContext.jsx'

// Capture the arguments observeBootStatus was called with so tests can
// inject events / disconnect callbacks at arbitrary times.
const captured = { onEvent: null, onDisconnect: null, signal: null, baseURL: null }
vi.mock('@/api/client-boot.js', () => ({
  observeBootStatus: vi.fn(({ baseURL, onEvent, onDisconnect, signal }) => {
    captured.baseURL = baseURL
    captured.onEvent = onEvent
    captured.onDisconnect = onDisconnect
    captured.signal = signal
    // Return a never-resolving promise so the component keeps its
    // subscription open for the duration of the test.
    return new Promise(() => {})
  }),
}))

import BootStatusStepper from './BootStatusStepper.jsx'

function renderStepper(overrides = {}) {
  return render(
    <I18nProvider>
      <BootStatusStepper baseURL="http://h" {...overrides} />
    </I18nProvider>,
  )
}

describe('BootStatusStepper', () => {
  beforeEach(() => {
    captured.onEvent = null
    captured.onDisconnect = null
    captured.signal = null
    captured.baseURL = null
    vi.useFakeTimers()
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('renders every known step as pending on first mount', () => {
    renderStepper()
    const items = screen.getAllByRole('listitem')
    // 20 steps in the canonical list (see bootSteps in component).
    expect(items.length).toBe(20)
    // All pending initially.
    for (const li of items) {
      expect(li.getAttribute('data-state')).toBe('pending')
    }
  })

  it('marks prior steps done and current step in-progress as events arrive', () => {
    renderStepper()
    act(() => captured.onEvent({ step: 'open_db' }))

    const setupTempDir = document.querySelector('[data-step="setup_temp_dir"]')
    const createDirs = document.querySelector('[data-step="create_dirs"]')
    const openDB = document.querySelector('[data-step="open_db"]')
    const reconcile = document.querySelector('[data-step="reconcile"]')

    expect(setupTempDir.getAttribute('data-state')).toBe('done')
    expect(createDirs.getAttribute('data-state')).toBe('done')
    expect(openDB.getAttribute('data-state')).toBe('in_progress')
    expect(reconcile.getAttribute('data-state')).toBe('pending')
  })

  it('shows per-package label for refreshing_<pkg> events', () => {
    renderStepper()
    act(() => captured.onEvent({ step: 'refreshing_core/gitea' }))
    expect(screen.getByText(/core\/gitea/)).toBeTruthy()
    const refreshRow = document.querySelector('[data-step="refresh_packages"]')
    expect(refreshRow.getAttribute('data-state')).toBe('in_progress')
  })

  it('marks ready as final step and calls onComplete on done', () => {
    const onComplete = vi.fn()
    renderStepper({ onComplete })
    act(() => captured.onEvent({ done: true }))
    expect(onComplete).toHaveBeenCalledTimes(1)
    const ready = document.querySelector('[data-step="ready"]')
    expect(ready.getAttribute('data-state')).toBe('in_progress')
  })

  it('surfaces reconnect banner on disconnect', () => {
    renderStepper()
    act(() => captured.onDisconnect(4000))
    expect(screen.getByTestId('boot-reconnect-banner')).toBeTruthy()
    expect(screen.getByText(/reconnecting in 4s/i)).toBeTruthy()
  })

  it('shows the mDNS hint after 60s of disconnection', () => {
    renderStepper({ hostname: 'town-os.local' })
    act(() => captured.onDisconnect(2000))
    expect(screen.queryByTestId('boot-mdns-hint')).toBeNull()
    act(() => { vi.advanceTimersByTime(60_000) })
    expect(screen.getByTestId('boot-mdns-hint')).toBeTruthy()
    // Hint references the hostname explicitly.
    expect(screen.getByTestId('boot-mdns-hint').textContent).toMatch(/town-os\.local/)
  })

  it('clears the reconnect banner when a step event arrives', () => {
    renderStepper()
    act(() => captured.onDisconnect(2000))
    expect(screen.getByTestId('boot-reconnect-banner')).toBeTruthy()
    act(() => captured.onEvent({ step: 'reconcile' }))
    expect(screen.queryByTestId('boot-reconnect-banner')).toBeNull()
  })

  it('surfaces an error banner when the stream emits an error frame', () => {
    const onError = vi.fn()
    renderStepper({ onError })
    act(() => captured.onEvent({ error: 'reconcile blew up' }))
    expect(onError).toHaveBeenCalledWith('reconcile blew up')
    expect(screen.getByTestId('boot-error-banner').textContent).toMatch(/reconcile blew up/)
  })
})
