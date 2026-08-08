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
      expect(mockSetDNSBL).toHaveBeenCalledWith(false, [{ zone: 'dbl.spamhaus.org', enabled: true }], 0)
    })
  })

  // A quick-add is an endorsement: one click and the operator is relying on the
  // zone. A decommissioned zone is a permanent no-op that reads as protection,
  // and a registration-gated one may answer for a while and then be cut off —
  // both fail silently, so they must never reappear in the suggestion lists.
  it('offers no decommissioned or registration-gated zones', async () => {
    // Nothing configured, so every suggestion renders as a quick-add.
    mockGetRBL.mockResolvedValue({ enabled: false, providers: [] })
    mockGetDNSBL.mockResolvedValue({ enabled: false, providers: [] })

    renderTab()
    await waitFor(() => expect(screen.getByText('Spamhaus DBL')).toBeTruthy())

    expect(screen.queryByText('SORBS')).toBeNull() // decommissioned 2024-06-05
    expect(screen.queryByText('Barracuda')).toBeNull() // requires IP registration
    expect(screen.getByText('Spam Eating Monkey')).toBeTruthy()
    expect(screen.getByText('PSBL')).toBeTruthy()
  })

  it('hides admin controls for non-admins', async () => {
    renderTab(false)
    await waitFor(() => expect(screen.getByText('zen.spamhaus.org')).toBeTruthy())
    // Suggestions / add-zone are admin-only.
    expect(screen.queryByText('Spamhaus DBL')).toBeNull()
    expect(screen.queryByText('Add zone')).toBeNull()
  })

  // --- Refusal codes ---
  //
  // A provider answers a refusal ("you are over your query limit") and a listing
  // with the same kind of record; only the address separates them. These cover
  // the operator's view of that: what is being backed off, and how to say what a
  // refusal looks like for a given provider.

  it('reports providers backed off after refusing a query', async () => {
    mockGetRBL.mockResolvedValue({
      enabled: true,
      providers: [{ zone: 'zen.spamhaus.org', enabled: true }],
      refusal_cooldown_secs: 3600,
      rotated_out: [{ zone: 'zen.spamhaus.org', code: '127.255.255.254', seconds_remaining: 3212 }],
    })

    renderTab()
    await waitFor(() => expect(screen.getByText('Not being queried right now')).toBeTruthy())
    // The zone, the code it refused with, and a readable time — 3212 seconds
    // rendered raw would leave the reader doing arithmetic to find out whether
    // this is a blip or the rest of the afternoon.
    expect(screen.getByText(/zen\.spamhaus\.org answered 127\.255\.255\.254 — back in 54m/)).toBeTruthy()
  })

  it('says nothing when no provider has refused', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('zen.spamhaus.org')).toBeTruthy())
    expect(screen.queryByText('Not being queried right now')).toBeNull()
  })

  // The resolved codes GET reports must NOT be echoed back on an unrelated save.
  // Doing so freezes today's built-in list into the stored config, so a code
  // rolodex adds later starts being read as a listing — the exact failure the
  // feature exists to prevent, reintroduced by the UI.
  it('does not freeze the built-in codes when saving an unrelated change', async () => {
    mockGetRBL.mockResolvedValue({
      enabled: true,
      providers: [
        {
          zone: 'zen.spamhaus.org',
          enabled: true,
          // What the server reports as in effect for a provider that named none.
          refusal_codes: ['127.255.255.0/24', '127.0.1.255', '127.0.2.255', '127.0.0.1', '127.0.0.255'],
        },
      ],
      refusal_cooldown_secs: 3600,
    })

    renderTab()
    await waitFor(() => expect(screen.getByText('zen.spamhaus.org')).toBeTruthy())

    fireEvent.click(screen.getByLabelText('zen.spamhaus.org')) // the enable switch
    await waitFor(() => expect(mockSetRBL).toHaveBeenCalled())
    expect(mockSetRBL).toHaveBeenCalledWith(
      true,
      [{ zone: 'zen.spamhaus.org', enabled: false }],
      3600,
    )
  })

  // A provider whose codes were spelled out explicitly is a different case: those
  // ARE the operator's choice and must survive an unrelated save intact.
  it('preserves explicitly configured codes across an unrelated change', async () => {
    mockGetRBL.mockResolvedValue({
      enabled: true,
      providers: [
        { zone: 'rbl.example.com', enabled: true, refusal_codes: ['127.0.0.9'], refusal_cooldown_secs: 60 },
      ],
      refusal_cooldown_secs: 3600,
    })

    renderTab()
    await waitFor(() => expect(screen.getByText('rbl.example.com')).toBeTruthy())

    fireEvent.click(screen.getByLabelText('rbl.example.com'))
    await waitFor(() => expect(mockSetRBL).toHaveBeenCalled())
    expect(mockSetRBL).toHaveBeenCalledWith(
      true,
      [{ zone: 'rbl.example.com', enabled: false, refusal_codes: ['127.0.0.9'], refusal_cooldown_secs: 60 }],
      3600,
    )
  })

  it('lets an admin spell out what a refusal looks like for one provider', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('zen.spamhaus.org')).toBeTruthy())

    fireEvent.click(screen.getByLabelText('Refusal codes for zen.spamhaus.org'))
    await waitFor(() => expect(screen.getByText('Refusal codes — zen.spamhaus.org')).toBeTruthy())

    fireEvent.click(screen.getByLabelText(/Custom codes/))
    fireEvent.change(screen.getByLabelText('Codes'), {
      target: { value: '127.255.255.0/24, 127.0.0.1' },
    })
    fireEvent.change(screen.getByLabelText('Stop querying it for (seconds)'), {
      target: { value: '900' },
    })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => expect(mockSetRBL).toHaveBeenCalled())
    expect(mockSetRBL).toHaveBeenCalledWith(
      true,
      [
        {
          zone: 'zen.spamhaus.org',
          enabled: true,
          refusal_codes: ['127.255.255.0/24', '127.0.0.1'],
          refusal_cooldown_secs: 900,
        },
      ],
      0,
    )
  })

  // The opt-out has to be expressible: a private blocklist may genuinely list
  // one of the built-in codes, and without this the only remedy would be to
  // disable the provider outright.
  it('lets an admin switch refusal detection off for one provider', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('zen.spamhaus.org')).toBeTruthy())

    fireEvent.click(screen.getByLabelText('Refusal codes for zen.spamhaus.org'))
    await waitFor(() => expect(screen.getByText('Refusal codes — zen.spamhaus.org')).toBeTruthy())

    fireEvent.click(screen.getByLabelText(/Treat every answer as a listing/))
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => expect(mockSetRBL).toHaveBeenCalled())
    expect(mockSetRBL).toHaveBeenCalledWith(
      true,
      [{ zone: 'zen.spamhaus.org', enabled: true, refusal_codes: ['none'] }],
      0,
    )
  })

  it('marks a provider whose refusal detection is off', async () => {
    mockGetRBL.mockResolvedValue({
      enabled: true,
      providers: [{ zone: 'rbl.example.com', enabled: true, refusal_codes: ['none'] }],
    })
    renderTab()
    await waitFor(() => expect(screen.getByText('refusal detection off')).toBeTruthy())
  })
})
