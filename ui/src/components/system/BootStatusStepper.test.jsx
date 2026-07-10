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
import { bootSteps } from './boot-steps.js'

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
    // One row per entry in the canonical list (see bootSteps).
    expect(items.length).toBe(bootSteps.length)
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

  // Regression: the backend emits several networking/ingress stages
  // (init_network_mgr, start_ingress, start_pages, reconcile_networks,
  // reconcile_ingress) that used to be missing from bootSteps. An
  // unrecognized step resolves to indexOf === -1, which stateFor treats
  // as "nothing done yet" and blanks every completed row. Every step the
  // backend can emit must be present so completed rows stay checked.
  for (const step of [
    'init_network_mgr',
    'start_ingress',
    'start_pages',
    'reconcile_networks',
    'reconcile_ingress',
  ]) {
    it(`keeps prior stages checked when the ${step} stage arrives`, () => {
      renderStepper()
      act(() => captured.onEvent({ step }))

      const idx = bootSteps.indexOf(step)
      expect(idx).toBeGreaterThan(0) // step is known, not -1
      // Everything before it must read as done, not reset to pending.
      for (let i = 0; i < idx; i++) {
        const row = document.querySelector(`[data-step="${bootSteps[i]}"]`)
        expect(row.getAttribute('data-state')).toBe('done')
      }
      const current = document.querySelector(`[data-step="${step}"]`)
      expect(current.getAttribute('data-state')).toBe('in_progress')
    })
  }

  it('advances monotonically through the full boot sequence without regressing', () => {
    renderStepper()
    // Play every stage in canonical order. After each one, no already-passed
    // stage may fall back to pending (the flicker bug).
    for (let cur = 0; cur < bootSteps.length; cur++) {
      act(() => captured.onEvent({ step: bootSteps[cur] }))
      for (let i = 0; i < bootSteps.length; i++) {
        const row = document.querySelector(`[data-step="${bootSteps[i]}"]`)
        const expected = i < cur ? 'done' : i === cur ? 'in_progress' : 'pending'
        expect(row.getAttribute('data-state')).toBe(expected)
      }
    }
  })

  it('ignores an unknown step instead of blanking completed rows', () => {
    renderStepper()
    // Advance a few real steps so there is progress to protect.
    act(() => captured.onEvent({ step: 'pull_images' }))
    const before = document.querySelector('[data-step="open_db"]').getAttribute('data-state')
    expect(before).toBe('done')

    // A step this build doesn't know about must not reset progress.
    act(() => captured.onEvent({ step: 'some_future_backend_step' }))
    expect(document.querySelector('[data-step="open_db"]').getAttribute('data-state')).toBe('done')
    expect(document.querySelector('[data-step="pull_images"]').getAttribute('data-state')).toBe('in_progress')
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
