import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

let mockPingResponse = null

const mockPing = vi.fn(() => Promise.resolve(mockPingResponse))
const mockDismissUpgrades = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    ping: mockPing,
    dismissUpgrades: mockDismissUpgrades,
  }),
}))

import DashboardHome from './DashboardHome.jsx'

function renderDashboard() {
  return render(
    <MemoryRouter>
      <DashboardHome />
    </MemoryRouter>,
  )
}

describe('DashboardHome', () => {
  beforeEach(() => {
    mockPing.mockReset()
    mockDismissUpgrades.mockReset()
    mockPingResponse = {
      status: 'ok',
      filesystems: 3,
      accounts: 2,
      units: { active: 5, total: 8, failed: 0 },
      packages: 10,
      installed: 4,
      repositories: 2,
      recent_errors: 0,
      upgrades_available: 0,
      upgrades_dismissed: false,
      external_ip: '1.2.3.4',
      internal_ip: '192.168.1.10',
    }
    mockPing.mockImplementation(() => Promise.resolve(mockPingResponse))
    mockDismissUpgrades.mockImplementation(() => Promise.resolve())
  })

  it('renders the dashboard title', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
  })

  it('renders the dashboard description', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('System overview and quick navigation')).toBeTruthy()
    })
  })

  it('displays external and internal IP addresses', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1.2.3.4')).toBeTruthy()
      expect(screen.getByText('192.168.1.10')).toBeTruthy()
    })
  })

  it('renders stat cards', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Filesystems')).toBeTruthy()
      expect(screen.getByText('Accounts')).toBeTruthy()
      expect(screen.getByText('Services')).toBeTruthy()
      expect(screen.getByText('Packages')).toBeTruthy()
      expect(screen.getByText('Repositories')).toBeTruthy()
      expect(screen.getByText('Audit Log')).toBeTruthy()
    })
  })

  it('displays filesystem count', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('3')).toBeTruthy()
    })
  })

  it('displays services as active / total', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('5 / 8')).toBeTruthy()
    })
  })

  it('shows no errors when recent_errors is 0', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('No errors')).toBeTruthy()
    })
  })

  it('shows error count when recent_errors > 0', async () => {
    mockPingResponse = { ...mockPingResponse, recent_errors: 3 }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('3 errors')).toBeTruthy()
    })
  })

  it('shows singular error for 1 error', async () => {
    mockPingResponse = { ...mockPingResponse, recent_errors: 1 }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1 error')).toBeTruthy()
    })
  })

  it('shows upgrade banner when upgrades are available', async () => {
    mockPingResponse = { ...mockPingResponse, upgrades_available: 3, upgrades_dismissed: false }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('3 package upgrades available')).toBeTruthy()
    })
  })

  it('shows singular upgrade banner for 1 upgrade', async () => {
    mockPingResponse = { ...mockPingResponse, upgrades_available: 1, upgrades_dismissed: false }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1 package upgrade available')).toBeTruthy()
    })
  })

  it('hides upgrade banner when dismissed', async () => {
    mockPingResponse = { ...mockPingResponse, upgrades_available: 2, upgrades_dismissed: true }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
    expect(screen.queryByText(/package upgrade/)).toBeNull()
  })

  it('shows failed services badge when units have failures', async () => {
    mockPingResponse = { ...mockPingResponse, units: { active: 5, total: 8, failed: 2 } }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('2 failed services')).toBeTruthy()
    })
  })

  it('shows singular failed service badge for 1 failure', async () => {
    mockPingResponse = { ...mockPingResponse, units: { active: 5, total: 8, failed: 1 } }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1 failed service')).toBeTruthy()
    })
  })

  it('hides failed services badge when no failures', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
    expect(screen.queryByText(/failed service/)).toBeNull()
  })

  it('shows loading state before data arrives', () => {
    mockPing.mockImplementation(() => new Promise(() => {}))
    renderDashboard()
    expect(screen.getByText('Loading...')).toBeTruthy()
  })

  it('displays installed count in package stat card', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('4 installed')).toBeTruthy()
    })
  })

  it('calls dismissUpgrades when dismiss button is clicked', async () => {
    mockPingResponse = { ...mockPingResponse, upgrades_available: 2, upgrades_dismissed: false }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('2 package upgrades available')).toBeTruthy()
    })
    const buttons = screen.getAllByRole('button')
    const dismissBtn = buttons.find((b) => b.closest('.border-blue-200'))
    fireEvent.click(dismissBtn)
    await waitFor(() => {
      expect(mockDismissUpgrades).toHaveBeenCalled()
    })
  })

  it('shows volume summary when installed/uninstalled volumes present', async () => {
    mockPingResponse = {
      ...mockPingResponse,
      disk_usage: null,
      installed_volumes: 3,
      uninstalled_volumes: 1,
    }
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('3 installed, 1 uninstalled volumes')).toBeTruthy()
    })
  })
})
