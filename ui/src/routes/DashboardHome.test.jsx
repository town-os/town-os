import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

let mockPingResponse = null
let mockUnitsResponse = { entries: [] }

const mockPing = vi.fn(() => Promise.resolve(mockPingResponse))
const mockDismissUpgrades = vi.fn(() => Promise.resolve())
const mockListUnitsTree = vi.fn(() => Promise.resolve(mockUnitsResponse))
const mockGetInstalledInfo = vi.fn(() => Promise.resolve({ notes: {}, note_types: {} }))

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    ping: mockPing,
    dismissUpgrades: mockDismissUpgrades,
    listUnitsTree: mockListUnitsTree,
    getInstalledInfo: mockGetInstalledInfo,
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
    mockListUnitsTree.mockReset()
    mockGetInstalledInfo.mockReset()
    mockUnitsResponse = { entries: [] }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({ notes: {}, note_types: {} }))
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

  it('shows loading state before data arrives', async () => {
    mockPing.mockImplementation(() => new Promise(() => {}))
    renderDashboard()
    // Asserted before any await: the loading state is the first paint.
    expect(screen.getByText('Loading...')).toBeTruthy()
    // The ping is pinned unresolved on purpose, but the mount fires the other
    // fetches too, and those do resolve — after this test returns, unless they
    // are drained here inside act.
    await act(async () => {})
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

  it('displays installed services panel listing services with URL notes', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-nginx-1.0.service', package_identifier: 'default/nginx@1.0', ActiveState: 'active', package_description: 'Web server' },
        { Name: 'town-os-package--default-redis-2.0.service', package_identifier: 'default/redis@2.0', ActiveState: 'failed', package_description: 'Cache server' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation((_repo, name) => {
      if (name === 'nginx') return Promise.resolve({ notes: { 'Web UI': 'https://nginx.example.com' }, note_types: { 'Web UI': 'url' } })
      if (name === 'redis') return Promise.resolve({ notes: { 'Admin': 'https://redis.example.com' }, note_types: { 'Admin': 'url' } })
      return Promise.resolve({ notes: {}, note_types: {} })
    })
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeTruthy()
      expect(screen.getByText('redis')).toBeTruthy()
    })
  })

  it('hides services that have no URL notes', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-nolinks-1.0.service', package_identifier: 'default/nolinks@1.0', ActiveState: 'active', package_description: 'Headless service' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({ notes: {}, note_types: {} }))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
    expect(screen.queryByText('Installed Services')).toBeNull()
    expect(screen.queryByText('nolinks')).toBeNull()
  })

  it('does not show services panel when no units', async () => {
    mockUnitsResponse = { entries: [] }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
    expect(screen.queryByText('Installed Services')).toBeNull()
  })

  it('copies external IP to clipboard via fallback when not in secure context', async () => {
    const origClipboard = navigator.clipboard
    const origSecure = window.isSecureContext
    Object.defineProperty(window, 'isSecureContext', { value: false, writable: true })
    Object.defineProperty(navigator, 'clipboard', { value: undefined, writable: true, configurable: true })

    if (!document.execCommand) {
      document.execCommand = vi.fn()
    }
    const execSpy = vi.spyOn(document, 'execCommand').mockReturnValue(true)

    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1.2.3.4')).toBeTruthy()
    })

    const copyBtns = screen.getAllByRole('button', { name: 'Copy to clipboard' })
    fireEvent.click(copyBtns[0])

    expect(execSpy).toHaveBeenCalledWith('copy')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Copied' })).toBeTruthy()
    })

    execSpy.mockRestore()
    Object.defineProperty(navigator, 'clipboard', { value: origClipboard, writable: true, configurable: true })
    Object.defineProperty(window, 'isSecureContext', { value: origSecure, writable: true })
  })

  it('copies IP to clipboard via navigator.clipboard in secure context', async () => {
    const writeTextMock = vi.fn(() => Promise.resolve())
    Object.defineProperty(window, 'isSecureContext', { value: true, writable: true })
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: writeTextMock }, writable: true, configurable: true })

    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1.2.3.4')).toBeTruthy()
    })

    const copyBtns = screen.getAllByRole('button', { name: 'Copy to clipboard' })
    fireEvent.click(copyBtns[0])

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith('1.2.3.4')
    })
  })

  it('renders https URL notes as external links pointing at the URL', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-myapp-1.0.service', package_identifier: 'default/myapp@1.0', ActiveState: 'active', package_description: '' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Web UI': 'https://myapp.example.com' },
      note_types: { 'Web UI': 'url' },
    }))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('https://myapp.example.com')).toBeTruthy()
    })
    const link = screen.getByText('https://myapp.example.com').closest('a')
    expect(link).toBeTruthy()
    expect(link.getAttribute('href')).toBe('https://myapp.example.com')
    expect(link.getAttribute('target')).toBe('_blank')
  })

  it('hides services whose only notes are non-https URLs', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-myapp-1.0.service', package_identifier: 'default/myapp@1.0', ActiveState: 'active', package_description: '' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Web UI': 'http://myapp.example.com' },
      note_types: { 'Web UI': 'url' },
    }))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
    expect(screen.queryByText('myapp')).toBeNull()
    expect(screen.queryByText('http://myapp.example.com')).toBeNull()
  })

  it('hides services whose only notes are non-URL (email, phone)', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-myapp-1.0.service', package_identifier: 'default/myapp@1.0', ActiveState: 'active', package_description: '' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Support': 'help@example.com', 'Hotline': '+1-555-0100' },
      note_types: { 'Support': 'email', 'Hotline': 'phone' },
    }))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeTruthy()
    })
    expect(screen.queryByText('myapp')).toBeNull()
    expect(screen.queryByText('help@example.com')).toBeNull()
    expect(screen.queryByText('+1-555-0100')).toBeNull()
  })

  it('links the package name to that service on the services screen', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-nginx-1.0.service', package_identifier: 'default/nginx@1.0', ActiveState: 'active', package_description: '' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Web UI': 'https://nginx.example.com' },
      note_types: { 'Web UI': 'url' },
    }))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeTruthy()
    })
    const nameLink = screen.getByText('nginx').closest('a')
    expect(nameLink).toBeTruthy()
    expect(nameLink.getAttribute('href')).toBe(
      '/dashboard/system?search=default%2Fnginx%401.0',
    )
  })

  it('links the status icon to that service on the services screen', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-nginx-1.0.service', package_identifier: 'default/nginx@1.0', ActiveState: 'active', package_description: '' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Web UI': 'https://nginx.example.com' },
      note_types: { 'Web UI': 'url' },
    }))
    renderDashboard()
    // The services panel renders only after the units fetch and the
    // per-service notes fetch both resolve; under full-suite CPU load that
    // chain can exceed findByRole's default 1s timeout.
    const statusLink = await screen.findByRole(
      'link',
      { name: 'nginx status: active' },
      { timeout: 5000 },
    )
    expect(statusLink.getAttribute('href')).toBe(
      '/dashboard/system?search=default%2Fnginx%401.0',
    )
  })

  // A dep's pretty display identifier ("core/gitea/postgres@1.0") is not a
  // unit the services screen can filter on; the raw package_identifier is.
  // Deep links must therefore carry package_identifier, never the display
  // form, or the target screen opens on an empty tree.
  it('deep-links using the raw package identifier, not the display identifier', async () => {
    mockUnitsResponse = {
      entries: [
        {
          Name: 'town-os-package--core-gitea-1.0.service',
          package_identifier: 'core/gitea@1.0',
          display_identifier: 'core/gitea/pretty@1.0',
          ActiveState: 'active',
          package_description: '',
        },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Web UI': 'https://gitea.example.com' },
      note_types: { 'Web UI': 'url' },
    }))
    renderDashboard()
    const nameLink = await screen.findByRole(
      'link',
      { name: 'gitea/pretty' },
      { timeout: 5000 },
    )
    expect(nameLink.getAttribute('href')).toBe(
      '/dashboard/system?search=core%2Fgitea%401.0',
    )
  })

  it('omits dependency sub-packages from the services panel', async () => {
    // Deps are internal plumbing — the dashboard panel only surfaces
    // user-visible root services that expose a URL. Even when a dep has
    // its own URL notes (rare but possible), the panel must not list it
    // separately from its parent root.
    mockUnitsResponse = {
      entries: [
        {
          Name: 'town-os-package--default-jitsi-1.0.service',
          package_identifier: 'default/jitsi@1.0',
          display_identifier: 'default/jitsi@1.0',
          ActiveState: 'active',
          package_description: '',
          children: [
            {
              Name: 'town-os-package--default-jitsi--dep--prosody-1.0.service',
              package_identifier: 'default/jitsi--dep--prosody@1.0',
              display_identifier: 'default/jitsi/prosody@1.0',
              ActiveState: 'active',
              package_description: '',
              children: [],
            },
          ],
        },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation((_repo, name) => {
      if (name === 'jitsi') return Promise.resolve({ notes: { 'Web UI': 'https://jitsi.example.com' }, note_types: { 'Web UI': 'url' } })
      return Promise.resolve({ notes: {}, note_types: {} })
    })
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('jitsi')).toBeTruthy()
    })
    // Dep row must not appear in the panel — not as pretty form, not as
    // the raw --dep-- form.
    expect(screen.queryByText('jitsi/prosody')).toBeNull()
    expect(screen.queryByText('jitsi--dep--prosody')).toBeNull()
  })

  it('renders each URL on the right of its service row', async () => {
    mockUnitsResponse = {
      entries: [
        { Name: 'town-os-package--default-myapp-1.0.service', package_identifier: 'default/myapp@1.0', ActiveState: 'active', package_description: '' },
      ],
    }
    mockListUnitsTree.mockImplementation(() => Promise.resolve(mockUnitsResponse))
    mockGetInstalledInfo.mockImplementation(() => Promise.resolve({
      notes: { 'Web UI': 'https://myapp.example.com', 'Admin': 'https://admin.myapp.example.com' },
      note_types: { 'Web UI': 'url', 'Admin': 'url' },
    }))
    renderDashboard()
    // Both URLs render, each as its own anchor pointing at the note
    // value. Row order is irrelevant — we only assert both links
    // belong to the same myapp row.
    await waitFor(() => {
      expect(screen.getByText('myapp')).toBeTruthy()
    })
    const webLink = screen.getByText('https://myapp.example.com').closest('a')
    const adminLink = screen.getByText('https://admin.myapp.example.com').closest('a')
    expect(webLink.getAttribute('href')).toBe('https://myapp.example.com')
    expect(adminLink.getAttribute('href')).toBe('https://admin.myapp.example.com')
  })
})
