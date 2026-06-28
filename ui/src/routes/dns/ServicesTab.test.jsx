import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockListDNSServices = vi.fn()
const mockSetDNSService = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listDNSServices: mockListDNSServices,
    setDNSService: mockSetDNSService,
  }),
}))

import ServicesTab from './ServicesTab.jsx'

function renderTab(isAdmin = true) {
  return render(
    <MemoryRouter>
      <ServicesTab isAdmin={isAdmin} />
    </MemoryRouter>,
  )
}

describe('ServicesTab', () => {
  beforeEach(() => {
    mockListDNSServices.mockReset()
    mockSetDNSService.mockClear()
    mockListDNSServices.mockResolvedValue([
      { repo: 'default', name: 'gitea', version: '1.0', fqdn: 'gitea.default.home', domains: [], published: true },
      { repo: 'default', name: 'redis', version: '2.0', fqdn: 'redis.default.home', domains: [], published: false },
    ])
  })

  it('renders installed services with their DNS names', async () => {
    renderTab()
    await waitFor(() => {
      expect(screen.getByText('gitea')).toBeTruthy()
      expect(screen.getByText('gitea.default.home')).toBeTruthy()
      expect(screen.getByText('redis.default.home')).toBeTruthy()
    })
  })

  it('toggles publish state via setDNSService', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('gitea')).toBeTruthy())

    // The first service (gitea) is published; clicking its switch unpublishes.
    const switches = screen.getAllByRole('switch')
    fireEvent.click(switches[0])

    await waitFor(() => {
      expect(mockSetDNSService).toHaveBeenCalledWith('default', 'gitea', false)
    })
  })

  it('disables switches for non-admins', async () => {
    renderTab(false)
    await waitFor(() => expect(screen.getByText('gitea')).toBeTruthy())
    for (const sw of screen.getAllByRole('switch')) {
      expect(sw.getAttribute('disabled')).not.toBeNull()
    }
  })
})
