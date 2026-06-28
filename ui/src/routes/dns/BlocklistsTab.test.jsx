import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockListBlocklists = vi.fn()
const mockGetRBL = vi.fn()
const mockGetDNSBL = vi.fn()
const mockListLocalRBL = vi.fn()
const mockApplyBlocklists = vi.fn(() => Promise.resolve({ status: 'started', feeds: [] }))
const mockClearBlocklists = vi.fn(() => Promise.resolve({ removed: 0 }))
const mockSetRBL = vi.fn(() => Promise.resolve())
const mockSetDNSBL = vi.fn(() => Promise.resolve())
const mockAddLocalRBL = vi.fn(() => Promise.resolve())
const mockRemoveLocalRBL = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listBlocklists: mockListBlocklists,
    getRBLConfig: mockGetRBL,
    getDNSBLConfig: mockGetDNSBL,
    listLocalRBL: mockListLocalRBL,
    applyBlocklists: mockApplyBlocklists,
    clearBlocklists: mockClearBlocklists,
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
    mockListBlocklists.mockReset()
    mockGetRBL.mockReset()
    mockGetDNSBL.mockReset()
    mockListLocalRBL.mockReset()
    mockApplyBlocklists.mockClear()
    mockClearBlocklists.mockClear()
    mockSetRBL.mockClear()

    mockListBlocklists.mockResolvedValue({
      feeds: [
        { key: 'oisd', name: 'OISD', description: 'Balanced.', url: 'https://big.oisd.nl/' },
        { key: 'hagezi', name: 'HaGeZi', description: 'Optimized.', url: 'https://example/hagezi' },
      ],
      running: false,
      status: [],
    })
    mockGetRBL.mockResolvedValue({ enabled: true, providers: [{ zone: 'zen.spamhaus.org', enabled: true }] })
    mockGetDNSBL.mockResolvedValue({ enabled: false, providers: [] })
    mockListLocalRBL.mockResolvedValue([{ name: 'ads.example.com', reason: 'blocklist:oisd' }])
  })

  it('renders curated feeds, provider zones, and local entries', async () => {
    renderTab()
    await waitFor(() => {
      expect(screen.getByText('OISD')).toBeTruthy()
      expect(screen.getByText('HaGeZi')).toBeTruthy()
      expect(screen.getByText('zen.spamhaus.org')).toBeTruthy()
      expect(screen.getByText('ads.example.com')).toBeTruthy()
    })
  })

  it('applies all feeds when "Apply all" is clicked', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('OISD')).toBeTruthy())

    fireEvent.click(screen.getByText('Apply all'))
    await waitFor(() => {
      expect(mockApplyBlocklists).toHaveBeenCalledWith({ keys: ['oisd', 'hagezi'] })
    })
  })

  it('hides admin controls for non-admins', async () => {
    renderTab(false)
    await waitFor(() => expect(screen.getByText('OISD')).toBeTruthy())
    expect(screen.queryByText('Apply all')).toBeNull()
  })
})
