import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
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
  Promise.resolve({ entries: [], cursor: null, end_cursor: null }),
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
      <TooltipProvider>
        <SystemManagement />
      </TooltipProvider>
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
      expect(mockLogTail).toHaveBeenCalledWith('town-os-foo.service', 200, undefined, undefined, undefined, undefined, undefined)
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
      expect(mockLogTail).toHaveBeenCalledWith('town-os-foo-network.service', 200, undefined, undefined, undefined, undefined, undefined)
    })
  })

  it('renders Controller Logs button', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('Controller Logs')).toBeTruthy()
    })
  })

  it('renders Advanced Logs button', async () => {
    renderSystemManagement()
    await waitFor(() => {
      expect(screen.getByText('Advanced Logs')).toBeTruthy()
    })
  })
})
