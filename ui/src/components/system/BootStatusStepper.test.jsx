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

const stateOf = (step) => document.querySelector(`[data-step="${step}"]`)?.getAttribute('data-state')

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

  it('renders exactly the five coarse stages, all pending, on first mount', () => {
    renderStepper()
    const items = screen.getAllByRole('listitem')
    // Five stages and nothing else: no package rows until the backend
    // actually names a package.
    expect(items.length).toBe(5)
    expect(bootSteps).toEqual([
      'boot_controller',
      'boot_dns',
      'boot_services',
      'restart_packages',
      'ready',
    ])
    for (const li of items) {
      expect(li.getAttribute('data-state')).toBe('pending')
    }
  })

  it('marks prior stages done and the current stage in-progress', () => {
    renderStepper()
    act(() => captured.onEvent({ step: 'boot_dns' }))

    expect(stateOf('boot_controller')).toBe('done')
    expect(stateOf('boot_dns')).toBe('in_progress')
    expect(stateOf('boot_services')).toBe('pending')
    expect(stateOf('restart_packages')).toBe('pending')
    expect(stateOf('ready')).toBe('pending')
  })

  it('advances monotonically through the full boot sequence without regressing', () => {
    renderStepper()
    // Play every stage in canonical order. After each one, no already-passed
    // stage may fall back to pending (the flicker bug).
    for (let cur = 0; cur < bootSteps.length; cur++) {
      act(() => captured.onEvent({ step: bootSteps[cur] }))
      for (let i = 0; i < bootSteps.length; i++) {
        const expected = i < cur ? 'done' : i === cur ? 'in_progress' : 'pending'
        expect(stateOf(bootSteps[i])).toBe(expected)
      }
    }
  })

  it('ignores an unknown step instead of blanking completed rows', () => {
    renderStepper()
    act(() => captured.onEvent({ step: 'boot_services' }))
    expect(stateOf('boot_controller')).toBe('done')

    // A step this build doesn't know about — including any of the retired
    // fine-grained names — must not reset progress.
    act(() => captured.onEvent({ step: 'some_future_backend_step' }))
    act(() => captured.onEvent({ step: 'open_db' }))
    expect(stateOf('boot_controller')).toBe('done')
    expect(stateOf('boot_services')).toBe('in_progress')
  })

  it('marks ready as the final stage and calls onComplete on done', () => {
    const onComplete = vi.fn()
    renderStepper({ onComplete })
    act(() => captured.onEvent({ done: true }))
    expect(onComplete).toHaveBeenCalledTimes(1)
    expect(stateOf('ready')).toBe('in_progress')
  })

  describe('per-package rows', () => {
    it('adds a row per package, each a peer of the five stages', () => {
      renderStepper()
      act(() => captured.onEvent({ step: 'restarting_core/gitea' }))
      act(() => captured.onEvent({ step: 'restarting_core/postgres' }))
      act(() => captured.onEvent({ step: 'restarting_extras/matrix' }))

      // Five stages + one row per package, in one flat list.
      expect(screen.getAllByRole('listitem').length).toBe(5 + 3)
      for (const pkg of ['core/gitea', 'core/postgres', 'extras/matrix']) {
        expect(document.querySelector(`[data-package="${pkg}"]`)).toBeTruthy()
        expect(screen.getByText(`Restarting ${pkg}`)).toBeTruthy()
      }
      // The package rows sit directly under the stage that owns them.
      const steps = [...document.querySelectorAll('li')].map((li) => li.getAttribute('data-step'))
      expect(steps).toEqual([
        'boot_controller',
        'boot_dns',
        'boot_services',
        'restart_packages',
        'restarting_core/gitea',
        'restarting_core/postgres',
        'restarting_extras/matrix',
        'ready',
      ])
    })

    it('tracks each package independently: earlier done, current in-progress', () => {
      renderStepper()
      act(() => captured.onEvent({ step: 'restarting_core/gitea' }))
      expect(stateOf('restart_packages')).toBe('in_progress')
      expect(stateOf('restarting_core/gitea')).toBe('in_progress')

      act(() => captured.onEvent({ step: 'restarting_core/postgres' }))
      expect(stateOf('restarting_core/gitea')).toBe('done')
      expect(stateOf('restarting_core/postgres')).toBe('in_progress')

      act(() => captured.onEvent({ step: 'restarting_extras/matrix' }))
      expect(stateOf('restarting_core/gitea')).toBe('done')
      expect(stateOf('restarting_core/postgres')).toBe('done')
      expect(stateOf('restarting_extras/matrix')).toBe('in_progress')
    })

    it('marks every package done once the boot moves past the restart stage', () => {
      renderStepper()
      act(() => captured.onEvent({ step: 'restarting_core/gitea' }))
      act(() => captured.onEvent({ step: 'restarting_core/postgres' }))
      act(() => captured.onEvent({ step: 'ready' }))

      expect(stateOf('restart_packages')).toBe('done')
      expect(stateOf('restarting_core/gitea')).toBe('done')
      expect(stateOf('restarting_core/postgres')).toBe('done')
      expect(stateOf('ready')).toBe('in_progress')
    })

    it('does not duplicate a package row when history replays after a reconnect', () => {
      renderStepper()
      act(() => captured.onEvent({ step: 'restarting_core/gitea' }))
      act(() => captured.onEvent({ step: 'restarting_core/postgres' }))
      // A reconnect replays the whole history, re-delivering both events.
      act(() => captured.onEvent({ step: 'restarting_core/gitea' }))
      act(() => captured.onEvent({ step: 'restarting_core/postgres' }))

      expect(document.querySelectorAll('[data-package="core/gitea"]').length).toBe(1)
      expect(screen.getAllByRole('listitem').length).toBe(5 + 2)
    })
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
    act(() => captured.onEvent({ step: 'boot_services' }))
    expect(screen.queryByTestId('boot-reconnect-banner')).toBeNull()
  })

  it('surfaces an error banner when the stream emits an error frame', () => {
    const onError = vi.fn()
    renderStepper({ onError })
    act(() => captured.onEvent({ error: 'reconcile blew up' }))
    expect(onError).toHaveBeenCalledWith('reconcile blew up')
    expect(screen.getByTestId('boot-error-banner').textContent).toMatch(/reconcile blew up/)
  })

  it('marks the failing package row as errored, not the whole stage list', () => {
    renderStepper()
    act(() => captured.onEvent({ step: 'restarting_core/gitea' }))
    act(() => captured.onEvent({ step: 'restarting_core/postgres' }))
    act(() => captured.onEvent({ error: 'unit failed to start' }))

    expect(stateOf('restarting_core/gitea')).toBe('done')
    expect(stateOf('restarting_core/postgres')).toBe('error')
    expect(stateOf('boot_controller')).toBe('done')
  })
})
