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

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listUnits: mockListUnits,
    logTail: mockLogTail,
    setUnitStatus: vi.fn(() => Promise.resolve()),
  }),
}))

function renderSystemManagement() {
  return render(
    <MemoryRouter>
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
    // Controller Logs is inside the Advanced Logs modal, not on the main page
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
      expect(screen.getByRole('button', { name: /Controller Logs/ })).toBeTruthy()
      expect(screen.getByRole('button', { name: /System Logs/ })).toBeTruthy()
      expect(screen.getByRole('button', { name: /Journal Errors/ })).toBeTruthy()
    })
  })

  it('opens controller logs from advanced modal', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Advanced Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Advanced Logs/ }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Controller Logs/ })).toBeTruthy()
    })

    fireEvent.click(screen.getByRole('button', { name: /Controller Logs/ }))

    await waitFor(() => {
      expect(mockLogTail).toHaveBeenCalledWith(
        'town-os-systemcontroller.service',
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
})
