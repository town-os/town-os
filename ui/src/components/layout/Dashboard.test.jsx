import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
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

describe('Dashboard sidebar pages gating', () => {
  beforeEach(() => {
    mockPingResponse = {
      status: 'ok',
      accounts: 1,
      admins: 1,
      needs_setup: false,
      locale: 'en-US',
      pages_enabled: false,
    }
  })

  it('hides the Pages nav entry when pages_enabled is false', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Services')).toBeTruthy()
    })
    expect(screen.queryByText('Pages')).toBeNull()
  })

  it('shows the Pages nav entry when pages_enabled is true', async () => {
    mockPingResponse = { ...mockPingResponse, pages_enabled: true }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Pages')).toBeTruthy()
    })
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
