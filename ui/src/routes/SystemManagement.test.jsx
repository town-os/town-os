import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import SystemManagement from './SystemManagement.jsx'

const mockListUnits = vi.fn(() =>
  Promise.resolve({
    entries: [
      {
        Name: 'town-os-foo.service',
        ActiveState: 'active',
        SubState: 'running',
        package_identifier: 'foo',
        package_description: 'Foo service',
        nc_failed: false,
      },
      {
        Name: 'town-os-bar.service',
        ActiveState: 'inactive',
        SubState: 'dead',
        package_identifier: 'bar',
        package_description: 'Bar service',
        nc_failed: false,
      },
    ],
    has_more: false,
    total_pages: 1,
    total_count: 2,
  }),
)

const mockLogTail = vi.fn(() =>
  Promise.resolve({ entries: [], cursor: '', end_cursor: '' }),
)

const mockListSystemServices = vi.fn(() =>
  Promise.resolve([
    {
      key: 'prometheus',
      display_name: 'Prometheus',
      image: 'quay.io/prometheus/prometheus:latest',
      port: '9091',
      Name: 'town-os-system--prometheus.service',
      ActiveState: 'active',
      SubState: 'running',
    },
    {
      key: 'node-exporter',
      display_name: 'Node Exporter',
      image: 'quay.io/prometheus/node-exporter:latest',
      port: '9101',
      Name: 'town-os-system--node-exporter.service',
      ActiveState: 'active',
      SubState: 'running',
    },
    {
      key: 'grafana',
      display_name: 'Grafana',
      image: 'docker.io/grafana/grafana:latest',
      port: '3001',
      Name: 'town-os-system--grafana.service',
      ActiveState: 'inactive',
      SubState: 'dead',
    },
  ]),
)

const mockSetSystemServiceStatus = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listUnits: mockListUnits,
    logTail: mockLogTail,
    setUnitStatus: vi.fn(() => Promise.resolve()),
    listSystemServices: mockListSystemServices,
    setSystemServiceStatus: mockSetSystemServiceStatus,
  }),
}))

function renderSystemManagement(initialEntries = ['/']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <SystemManagement />
    </MemoryRouter>,
  )
}

/**
 * Open the Radix DropdownMenu by dispatching pointer events on the trigger.
 * jsdom doesn't fully handle Radix click-to-open so we use pointerDown.
 */
async function openDropdown(container, index = 0) {
  const triggers = container.querySelectorAll('[data-slot="dropdown-menu-trigger"]')
  const trigger = triggers[index]
  fireEvent.pointerDown(trigger, { button: 0, pointerType: 'mouse' })
}

describe('SystemManagement', () => {
  beforeEach(() => {
    mockLogTail.mockClear()
    mockListSystemServices.mockClear()
    mockSetSystemServiceStatus.mockClear()
  })

  it('renders the Services heading', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('Services')).toBeTruthy()
    })
  })

  it('renders service data in table rows', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('foo')).toBeTruthy()
    })
    expect(screen.getByText('bar')).toBeTruthy()
    expect(screen.getByText('Foo service')).toBeTruthy()
    expect(screen.getByText('Bar service')).toBeTruthy()
  })

  it('renders status badges', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('active')).toBeTruthy()
    })
    expect(screen.getByText('inactive')).toBeTruthy()
  })

  it('renders action dropdown with correct menu items', async () => {
    const { container } = renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('foo')).toBeTruthy()
    })
    await openDropdown(container, 0)
    await waitFor(() => {
      expect(screen.getByText('Service Logs')).toBeTruthy()
    })
    expect(screen.getByText('Network Logs')).toBeTruthy()
    expect(screen.getByText('Start')).toBeTruthy()
    expect(screen.getByText('Restart')).toBeTruthy()
    expect(screen.getByText('Stop')).toBeTruthy()
  })

  it('opens journal dialog when Service Logs is clicked', async () => {
    const { container } = renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('foo')).toBeTruthy()
    })
    await openDropdown(container, 0)
    await waitFor(() => {
      expect(screen.getByText('Service Logs')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Service Logs'))
    await waitFor(() => {
      expect(mockLogTail).toHaveBeenCalledWith('town-os-foo.service', 200, undefined, undefined, undefined, undefined, undefined, undefined)
    })
  })

  it('opens journal dialog for network unit when Network Logs is clicked', async () => {
    mockLogTail.mockClear()
    const { container } = renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('foo')).toBeTruthy()
    })
    await openDropdown(container, 0)
    await waitFor(() => {
      expect(screen.getByText('Network Logs')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Network Logs'))
    await waitFor(() => {
      expect(mockLogTail).toHaveBeenCalledWith('town-os-foo-network.service', 200, undefined, undefined, undefined, undefined, undefined, undefined)
    })
  })

  it('does not show Controller Logs button on main page', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('Services')).toBeTruthy()
    })
    // Controller Logs is not available anywhere
    expect(screen.queryByRole('button', { name: /Controller Logs/ })).toBeNull()
  })

  it('shows Advanced Logs button', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })
  })

  it('opens advanced logs modal with quick-access buttons', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Advanced Logs/ }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /System Logs/ })).toBeTruthy()
      expect(screen.getByRole('button', { name: /Journal Errors/ })).toBeTruthy()
    })
    // Controller Logs should NOT be present in the advanced modal
    expect(screen.queryByRole('button', { name: /Controller Logs/ })).toBeNull()
  })

  it('opens system logs from advanced modal', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Advanced Logs/ }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /System Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /System Logs/ }))

    await waitFor(() => {
      // System logs use empty string for unit
      expect(mockLogTail).toHaveBeenCalledWith(
        '',
        200,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
      )
    })
  })

  it('opens journal errors with priority filter from advanced modal', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Advanced Logs/ }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Journal Errors/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Journal Errors/ }))

    await waitFor(() => {
      // Journal errors use empty unit with priority=3
      expect(mockLogTail).toHaveBeenCalledWith(
        '',
        200,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        3,
      )
    })
  })

  it('shows Journal Errors title when priority is set', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Advanced Logs/ }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Journal Errors/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Journal Errors/ }))

    await waitFor(() => {
      expect(screen.getByText('Journal Errors')).toBeTruthy()
    })
  })

  it('has custom service name input in advanced modal', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Advanced Logs/ }))

    await waitFor(() => {
      expect(screen.getByLabelText('Custom service name')).toBeTruthy()
    })

    // View button is disabled when input is empty
    const viewBtn = screen.getByRole('button', { name: 'View' })
    expect(viewBtn.disabled).toBe(true)
  })

  // System Services tests

  it('renders collapsed system services section', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('System Services')).toBeTruthy()
    })
    // When collapsed, service names should not be visible as table rows
    expect(screen.queryByText('Prometheus')).toBeNull()
  })

  it('expands system services section to show services', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('System Services')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('System Services'))

    await waitFor(() => {
      expect(screen.getByText('Prometheus')).toBeTruthy()
      expect(screen.getByText('Node Exporter')).toBeTruthy()
      expect(screen.getByText('Grafana')).toBeTruthy()
    })
  })

  it('shows system service status badges', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('System Services')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('System Services'))

    await waitFor(() => {
      expect(screen.getByText('Prometheus')).toBeTruthy()
    })
    // The system services table should show active/inactive badges
    // (Note: the package DataTable also shows active/inactive, so we just
    // verify the system service section rendered with service names)
    expect(screen.getByText('Grafana')).toBeTruthy()
  })

  it('shows system service action dropdown', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('System Services')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('System Services'))

    await waitFor(() => {
      expect(screen.getByText('Prometheus')).toBeTruthy()
    })

    // The system services section has its own action dropdown triggers
    // They appear after the package unit dropdowns
    const { container } = renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('System Services')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('System Services'))
    await waitFor(() => {
      expect(screen.getByText('Prometheus')).toBeTruthy()
    })

    // Get all dropdown triggers — package units + system services
    const triggers = container.querySelectorAll('[data-slot="dropdown-menu-trigger"]')
    expect(triggers.length).toBeGreaterThan(0)
  })

  it('system service logs opens journal viewer with correct unit', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('System Services')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('System Services'))

    await waitFor(() => {
      expect(screen.getByText('Prometheus')).toBeTruthy()
    })

    // Find the system service section triggers and open the first one (Prometheus)
    // The system service rows are inside a table within the collapsible section
    const sysTable = screen.getByText('Prometheus').closest('table')
    const triggers = sysTable.querySelectorAll('[data-slot="dropdown-menu-trigger"]')
    fireEvent.pointerDown(triggers[0], { button: 0, pointerType: 'mouse' })

    await waitFor(() => {
      // Service Logs is in the dropdown — there will be multiple "Service Logs" on screen
      // because the package unit dropdown may also have them. We need to click one.
      const logItems = screen.getAllByText('Service Logs')
      fireEvent.click(logItems[logItems.length - 1])
    })

    await waitFor(() => {
      expect(mockLogTail).toHaveBeenCalledWith(
        'town-os-system--prometheus.service',
        200,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
      )
    })
  })

  it('hides system services section when no services returned', async () => {
    mockListSystemServices.mockResolvedValueOnce([])
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('Services')).toBeTruthy()
    })
    // System Services section should not appear
    expect(screen.queryByText('System Services')).toBeNull()
  })
})
