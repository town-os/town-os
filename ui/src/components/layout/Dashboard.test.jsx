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
})
