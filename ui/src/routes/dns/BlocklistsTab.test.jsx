import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockGetRBL = vi.fn()
const mockGetDNSBL = vi.fn()
const mockListLocalRBL = vi.fn()
const mockSetRBL = vi.fn(() => Promise.resolve())
const mockSetDNSBL = vi.fn(() => Promise.resolve())
const mockAddLocalRBL = vi.fn(() => Promise.resolve())
const mockRemoveLocalRBL = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    getRBLConfig: mockGetRBL,
    getDNSBLConfig: mockGetDNSBL,
    listLocalRBL: mockListLocalRBL,
    setRBLConfig: mockSetRBL,
    setDNSBLConfig: mockSetDNSBL,
    addLocalRBL: mockAddLocalRBL,
    removeLocalRBL: mockRemoveLocalRBL,
  }),
}))

import BlocklistsTab from './BlocklistsTab.jsx'

function renderTab(isAdmin = true) {
  return render(
    <MemoryRouter>
      <BlocklistsTab isAdmin={isAdmin} />
    </MemoryRouter>,
  )
}

describe('BlocklistsTab', () => {
  beforeEach(() => {
    mockGetRBL.mockReset()
    mockGetDNSBL.mockReset()
    mockListLocalRBL.mockReset()
    mockSetRBL.mockClear()
    mockSetDNSBL.mockClear()

    mockGetRBL.mockResolvedValue({ enabled: true, providers: [{ zone: 'zen.spamhaus.org', enabled: true }] })
    mockGetDNSBL.mockResolvedValue({ enabled: false, providers: [] })
    mockListLocalRBL.mockResolvedValue([{ name: 'ads.example.com', reason: 'manual' }])
  })

  it('renders provider zones, suggestions, and local entries', async () => {
    renderTab()
    await waitFor(() => {
      // Configured RBL zone.
      expect(screen.getByText('zen.spamhaus.org')).toBeTruthy()
      // A suggested DNSBL zone (none configured yet) shows as a quick-add.
      expect(screen.getByText('Spamhaus DBL')).toBeTruthy()
      // Local entry.
      expect(screen.getByText('ads.example.com')).toBeTruthy()
    })
    // No feed apply controls exist anymore.
    expect(screen.queryByText('Apply all')).toBeNull()
  })

  it('adds a suggested DNSBL zone on demand (no caching)', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('Spamhaus DBL')).toBeTruthy())

    fireEvent.click(screen.getByText('Spamhaus DBL'))
    await waitFor(() => {
      expect(mockSetDNSBL).toHaveBeenCalledWith(false, [{ zone: 'dbl.spamhaus.org', enabled: true }])
    })
  })

  it('hides admin controls for non-admins', async () => {
    renderTab(false)
    await waitFor(() => expect(screen.getByText('zen.spamhaus.org')).toBeTruthy())
    // Suggestions / add-zone are admin-only.
    expect(screen.queryByText('Spamhaus DBL')).toBeNull()
    expect(screen.queryByText('Add zone')).toBeNull()
  })
})
