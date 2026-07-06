import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import Networks from './Networks.jsx'

if (!globalThis.localStorage || typeof globalThis.localStorage.removeItem !== 'function') {
  const store = {}
  globalThis.localStorage = {
    getItem: (k) => (k in store ? store[k] : null),
    setItem: (k, v) => { store[k] = String(v) },
    removeItem: (k) => { delete store[k] },
  }
}

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
})

const mockClient = {
  ping: vi.fn(() => Promise.resolve({ username: 'admin', admin: true })),
  listNetworks: vi.fn(() =>
    Promise.resolve([
      { name: 'home', tld: 'home', subnet: '10.90.1.0/24', address: '10.90.1.1/24', listen_port: 51820, enabled: true, peer_count: 0, interface: 'town1a2b', running: true },
      { name: 'office', tld: 'office', subnet: '10.90.2.0/24', address: '10.90.2.1/24', listen_port: 51821, enabled: false, peer_count: 2, interface: 'townc3d4', running: false },
    ]),
  ),
}

vi.mock('@/lib/client-instance.js', () => ({
  default: () => mockClient,
}))

function renderNetworks() {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <Networks />
      </TooltipProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  mockClient.listNetworks.mockClear()
})

describe('Networks', () => {
  it('renders the networks list with names and subnets', async () => {
    renderNetworks()
    await waitFor(() => {
      expect(screen.getByText('home')).toBeTruthy()
      expect(screen.getByText('office')).toBeTruthy()
    })
    // Subnet is shown for each network.
    expect(screen.getByText('10.90.1.0/24')).toBeTruthy()
    expect(screen.getByText('10.90.2.0/24')).toBeTruthy()
  })

  it('calls listNetworks on mount', async () => {
    renderNetworks()
    await waitFor(() => {
      expect(mockClient.listNetworks).toHaveBeenCalled()
    })
  })
})
