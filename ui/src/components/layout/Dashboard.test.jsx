import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

let mockPingResponse = null

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    ping: () => Promise.resolve(mockPingResponse),
  }),
}))

vi.mock('@/lib/auth.js', () => ({
  getToken: () => 'tok',
  getAccount: () => ({ username: 'admin', admin: true }),
  clearToken: vi.fn(),
  setSessionExpired: vi.fn(),
}))

import Dashboard from './Dashboard.jsx'

function renderDashboard() {
  return render(
    <MemoryRouter>
      <Dashboard>
        <div>child</div>
      </Dashboard>
    </MemoryRouter>,
  )
}

describe('Dashboard sidebar', () => {
  beforeEach(() => {
    mockPingResponse = {
      status: 'ok',
      accounts: 1,
      admins: 1,
      needs_setup: false,
      locale: 'en-US',
    }
  })

  // The pages subsystem is always initialized at boot — there is no feature
  // gate — so the Pages entry must render with the rest of the sidebar on the
  // very first paint. It used to be gated on a ping field, which made it pop
  // in a beat late (and never at all if the ping failed).
  it('renders the Pages nav entry immediately, before any ping resolves', async () => {
    renderDashboard()
    // Asserted before any await, which is the claim: the entry is in the first
    // paint and does not wait on the ping.
    expect(screen.getByText('Pages')).toBeTruthy()
    // Then drain the ping the mount kicked off. Without this the test returns
    // with the promise still in flight, it resolves against a component React
    // is no longer expecting updates for, and React warns about a state update
    // outside act — attributing it to whichever test happens to be running.
    await act(async () => {})
  })

  it('keeps the Pages nav entry when the ping fails', async () => {
    mockPingResponse = { status: 'error' }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Services')).toBeTruthy()
    })
    expect(screen.getByText('Pages')).toBeTruthy()
  })

  // Regression: without `min-w-0` on the right-pane flex column, a wide
  // child (e.g. a wide packages table) pushes the flex column past its
  // allocated share and the downstream overflow-x-auto wrappers never
  // engage — the packages list ends up wider than the dashboard "container"
  // the rest of the UI assumes. This test locks the class in place so a
  // future layout refactor can't strip it silently.
  it('right-pane flex column has min-w-0 so wide tables stay contained', async () => {
    const { container } = renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('child')).toBeTruthy()
    })
    const rightPane = container.querySelector('main')?.parentElement
    expect(rightPane).toBeTruthy()
    expect(rightPane.className).toMatch(/\bmin-w-0\b/)
    expect(rightPane.className).toMatch(/\bflex-1\b/)
  })
})
