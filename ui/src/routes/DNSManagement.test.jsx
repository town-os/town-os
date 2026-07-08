import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockDnsStatus = vi.fn(() =>
  Promise.resolve({ enabled: true, running: true, tld: 'town', record_count: 2 }),
)
const mockListDNSRecords = vi.fn(() =>
  Promise.resolve([
    { name: 'app.town', record_type: 0, value: '192.168.1.1', ttl: 300, priority: 0 },
    { name: 'mail.town', record_type: 3, value: 'mx.town', ttl: 600, priority: 10 },
  ]),
)
const mockAddDNSRecord = vi.fn(() => Promise.resolve())
const mockRemoveDNSRecord = vi.fn(() => Promise.resolve())
const mockGetDNSTLD = vi.fn(() => Promise.resolve({ tld: 'town' }))
const mockSetDNSTLD = vi.fn(() => Promise.resolve())
const mockSetupDNS = vi.fn(() => Promise.resolve())
const mockListNetworks = vi.fn(() =>
  Promise.resolve([
    { name: 'home', tld: 'town', enabled: true },
    { name: 'office', tld: 'office', enabled: true },
  ]),
)

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    dnsStatus: mockDnsStatus,
    listDNSRecords: mockListDNSRecords,
    addDNSRecord: mockAddDNSRecord,
    removeDNSRecord: mockRemoveDNSRecord,
    getDNSTLD: mockGetDNSTLD,
    setDNSTLD: mockSetDNSTLD,
    setupDNS: mockSetupDNS,
    listNetworks: mockListNetworks,
  }),
}))

const mockUseRequireAuth = vi.fn(() => ({ username: 'admin', admin: true }))

vi.mock('@/lib/hooks.js', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useRequireAuth: (...args) => mockUseRequireAuth(...args),
  }
})

import DNSManagement from './DNSManagement.jsx'

function renderDNS() {
  return render(
    <MemoryRouter>
      <DNSManagement />
    </MemoryRouter>,
  )
}

describe('DNSManagement component', () => {
  beforeEach(() => {
    mockDnsStatus.mockClear()
    mockListDNSRecords.mockClear()
    mockAddDNSRecord.mockClear()
    mockRemoveDNSRecord.mockClear()
    mockGetDNSTLD.mockClear()
    mockSetDNSTLD.mockClear()
    mockSetupDNS.mockClear()
    mockListNetworks.mockClear()

    mockDnsStatus.mockResolvedValue({ enabled: true, running: true, tld: 'town', record_count: 2 })
    mockListDNSRecords.mockResolvedValue([
      { name: 'app.town', record_type: 0, value: '192.168.1.1', ttl: 300, priority: 0, network: '', tld: 'town' },
      { name: 'mail.town', record_type: 3, value: 'mx.town', ttl: 600, priority: 10, network: '', tld: 'town' },
    ])
    mockListNetworks.mockResolvedValue([
      { name: 'home', tld: 'town', enabled: true },
      { name: 'office', tld: 'office', enabled: true },
    ])
  })

  it('renders the DNS heading', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('DNS')).toBeTruthy()
    })
  })

  it('renders the description', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Manage DNS records and service configuration')).toBeTruthy()
    })
  })

  it('sets document title', async () => {
    renderDNS()
    await waitFor(() => {
      expect(document.title).toBe('Town OS - DNS')
    })
  })

  it('calls dnsStatus on mount', async () => {
    renderDNS()
    await waitFor(() => {
      expect(mockDnsStatus).toHaveBeenCalled()
    })
  })

  it('calls listDNSRecords on mount', async () => {
    renderDNS()
    await waitFor(() => {
      expect(mockListDNSRecords).toHaveBeenCalled()
    })
  })

  it('fetches records across all networks by default', async () => {
    renderDNS()
    // An empty tld filter means "every network" (global + scoped).
    await waitFor(() => {
      expect(mockListDNSRecords).toHaveBeenCalledWith('')
    })
    // The TLD filter defaults to "All TLDs" and networks feed its options.
    await waitFor(() => {
      expect(mockListNetworks).toHaveBeenCalled()
    })
    expect(screen.getByText('All TLDs')).toBeTruthy()
  })

  it('shows each record\'s TLD/network', async () => {
    mockListDNSRecords.mockResolvedValue([
      { name: 'gitea.office', record_type: 0, value: '10.90.12.5', ttl: 300, priority: 0, network: 'office', tld: 'office' },
    ])
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('gitea.office')).toBeTruthy()
    })
    // The scoped record is labelled with its owning network.
    expect(screen.getByText('office')).toBeTruthy()
  })

  it('displays status badges', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Enabled')).toBeTruthy()
      expect(screen.getByText('Running')).toBeTruthy()
    })
  })

  it('displays TLD in status card', async () => {
    renderDNS()
    // "town" now appears both in the status card and in each record's TLD
    // badge; target the status-card span (text-sm font-medium) specifically.
    await waitFor(() => {
      const matches = screen.getAllByText('town')
      expect(
        matches.some(
          (el) => el.className.includes('text-sm') && el.className.includes('font-medium'),
        ),
      ).toBe(true)
    })
  })

  it('displays record count', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('2 records')).toBeTruthy()
    })
  })

  it('displays record names in table', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('app.town')).toBeTruthy()
      expect(screen.getByText('mail.town')).toBeTruthy()
    })
  })

  it('displays record type badges', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('A')).toBeTruthy()
      expect(screen.getByText('MX')).toBeTruthy()
    })
  })

  it('displays record values', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('192.168.1.1')).toBeTruthy()
      expect(screen.getByText('mx.town')).toBeTruthy()
    })
  })

  it('displays TTL values', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('300')).toBeTruthy()
      expect(screen.getByText('600')).toBeTruthy()
    })
  })

  it('renders admin buttons (Setup DNS, Add Record)', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Setup DNS')).toBeTruthy()
      expect(screen.getByText('Add Record')).toBeTruthy()
    })
  })

  it('renders Change TLD button for admin', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Change')).toBeTruthy()
    })
  })

  it('shows disabled state badges', async () => {
    mockDnsStatus.mockResolvedValueOnce({ enabled: false, running: false, tld: '', record_count: 0 })
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Disabled')).toBeTruthy()
      expect(screen.getByText('Stopped')).toBeTruthy()
    })
  })

  it('shows disabled message when DNS is not enabled', async () => {
    mockDnsStatus.mockResolvedValueOnce({ enabled: false, running: false, tld: '', record_count: 0 })
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText(/DNS service is not enabled/)).toBeTruthy()
    })
  })

  it('shows loading state when no data', async () => {
    mockDnsStatus.mockReturnValueOnce(new Promise(() => {}))
    mockListDNSRecords.mockReturnValueOnce(new Promise(() => {}))
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Loading...')).toBeTruthy()
    })
  })

  it('shows empty table when no records exist', async () => {
    mockListDNSRecords.mockResolvedValueOnce([])
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('No data')).toBeTruthy()
    })
  })

  it('opens setup DNS confirmation dialog', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Setup DNS')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Setup DNS'))
    await waitFor(() => {
      expect(screen.getByText(/idempotent and safe to run/)).toBeTruthy()
    })
  })

  it('calls setupDNS when confirmed', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Setup DNS')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Setup DNS'))
    await waitFor(() => {
      expect(screen.getByText('Run Setup')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Run Setup'))
    await waitFor(() => {
      expect(mockSetupDNS).toHaveBeenCalled()
    })
  })

  it('opens remove record confirmation dialog', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('app.town')).toBeTruthy()
    })
    // Click the first remove button (trash icon)
    // Find the trash buttons in the table
    const trashBtns = screen.getByText('app.town').closest('tr')?.querySelectorAll('button')
    if (trashBtns && trashBtns.length > 0) {
      fireEvent.click(trashBtns[trashBtns.length - 1])
    }
    await waitFor(() => {
      expect(screen.getByText('Remove DNS Record')).toBeTruthy()
    })
  })

  it('calls removeDNSRecord when confirmed and record disappears', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('app.town')).toBeTruthy()
    })
    const trashBtns = screen.getByText('app.town').closest('tr')?.querySelectorAll('button')
    if (trashBtns && trashBtns.length > 0) {
      fireEvent.click(trashBtns[trashBtns.length - 1])
    }
    await waitFor(() => {
      expect(screen.getByText('Remove DNS Record')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Remove'))
    await waitFor(() => {
      expect(mockRemoveDNSRecord).toHaveBeenCalledWith('app.town', 0)
    })
    // Record should be optimistically removed from the table
    await waitFor(() => {
      expect(screen.queryByText('app.town')).toBeNull()
    })
    // Other record should still be visible
    expect(screen.getByText('mail.town')).toBeTruthy()
  })

  it('opens change TLD dialog', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Change')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Change'))
    await waitFor(() => {
      expect(screen.getAllByText('Change TLD').length).toBeGreaterThanOrEqual(1)
      expect(screen.getByText(/re-provision the DNS zone/)).toBeTruthy()
    })
  })

  it('calls setDNSTLD when TLD change is submitted', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Change')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Change'))
    await waitFor(() => {
      expect(screen.getByLabelText('New TLD')).toBeTruthy()
    })
    fireEvent.change(screen.getByLabelText('New TLD'), { target: { value: 'example' } })
    // Click the submit button in the dialog (last "Change TLD" text is the submit button)
    const submitBtns = screen.getAllByRole('button').filter((b) => b.textContent === 'Change TLD')
    fireEvent.click(submitBtns[submitBtns.length - 1])
    await waitFor(() => {
      expect(mockSetDNSTLD).toHaveBeenCalledWith('example')
    })
  })

  it('singular record count for 1 record', async () => {
    mockDnsStatus.mockResolvedValueOnce({ enabled: true, running: true, tld: 'town', record_count: 1 })
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('1 record')).toBeTruthy()
    })
  })

  it('maps all known record types to display strings', async () => {
    mockListDNSRecords.mockResolvedValueOnce([
      { name: 'a.town', record_type: 0, value: '1.2.3.4', ttl: 300, priority: 0 },
      { name: 'aaaa.town', record_type: 1, value: '::1', ttl: 300, priority: 0 },
      { name: 'cname.town', record_type: 2, value: 'alias.town', ttl: 300, priority: 0 },
      { name: 'txt.town', record_type: 4, value: 'v=spf1', ttl: 300, priority: 0 },
    ])
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('A')).toBeTruthy()
      expect(screen.getByText('AAAA')).toBeTruthy()
      expect(screen.getByText('CNAME')).toBeTruthy()
      expect(screen.getByText('TXT')).toBeTruthy()
    })
  })
})

describe('DNSManagement add record dialog', () => {
  beforeAll(() => {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    HTMLElement.prototype.hasPointerCapture = () => false
    HTMLElement.prototype.setPointerCapture = () => {}
    HTMLElement.prototype.releasePointerCapture = () => {}
    HTMLElement.prototype.scrollIntoView = () => {}
  })

  beforeEach(() => {
    mockDnsStatus.mockClear()
    mockListDNSRecords.mockClear()
    mockAddDNSRecord.mockClear()

    mockDnsStatus.mockResolvedValue({ enabled: true, running: true, tld: 'town', record_count: 0 })
    mockListDNSRecords.mockResolvedValue([])
    mockAddDNSRecord.mockResolvedValue()
  })

  it('opens add record dialog when Add Record button is clicked', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Add Record')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Add Record'))
    await waitFor(() => {
      expect(screen.getByText('Add DNS Record')).toBeTruthy()
      expect(screen.getByLabelText('Name')).toBeTruthy()
    })
  })

  it('has type selector with common record types', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Add Record')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Add Record'))
    await waitFor(() => {
      expect(screen.getByText('Add DNS Record')).toBeTruthy()
    })
    // Open the select dropdown
    const trigger = screen.getByRole('combobox')
    fireEvent.pointerDown(trigger, { button: 0, pointerType: 'mouse' })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'A' })).toBeTruthy()
      expect(screen.getByRole('option', { name: 'AAAA' })).toBeTruthy()
      expect(screen.getByRole('option', { name: 'CNAME' })).toBeTruthy()
      expect(screen.getByRole('option', { name: 'MX' })).toBeTruthy()
      expect(screen.getByRole('option', { name: 'TXT' })).toBeTruthy()
      expect(screen.getByRole('option', { name: 'SRV' })).toBeTruthy()
      expect(screen.getByRole('option', { name: 'PTR' })).toBeTruthy()
    })
  })

  it('submit button disabled when no type selected', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Add Record')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Add Record'))
    await waitFor(() => {
      expect(screen.getByText('Add DNS Record')).toBeTruthy()
    })
    // The submit button inside the dialog
    const submitBtns = screen.getAllByRole('button').filter((b) => b.textContent === 'Add Record')
    const dialogSubmit = submitBtns[submitBtns.length - 1]
    expect(dialogSubmit.disabled).toBe(true)
  })

  it('calls addDNSRecord when form is submitted', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Add Record')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Add Record'))
    await waitFor(() => {
      expect(screen.getByText('Add DNS Record')).toBeTruthy()
    })

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'test.town' } })
    fireEvent.change(screen.getByLabelText('Value'), { target: { value: '10.0.0.1' } })

    // Select type
    const trigger = screen.getByRole('combobox')
    fireEvent.pointerDown(trigger, { button: 0, pointerType: 'mouse' })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'A' })).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('option', { name: 'A' }))

    // Submit
    const submitBtns = screen.getAllByRole('button').filter((b) => b.textContent === 'Add Record')
    fireEvent.click(submitBtns[submitBtns.length - 1])

    await waitFor(() => {
      expect(mockAddDNSRecord).toHaveBeenCalledWith('test.town', 0, '10.0.0.1', 300)
    })
  })

  it('has default TTL of 300', async () => {
    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('Add Record')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Add Record'))
    await waitFor(() => {
      expect(screen.getByLabelText('TTL (seconds)')).toBeTruthy()
    })
    expect(screen.getByLabelText('TTL (seconds)').value).toBe('300')
  })
})

describe('DNSManagement non-admin view', () => {
  beforeEach(() => {
    mockDnsStatus.mockClear()
    mockListDNSRecords.mockClear()

    mockDnsStatus.mockResolvedValue({ enabled: true, running: true, tld: 'town', record_count: 1 })
    mockListDNSRecords.mockResolvedValue([
      { name: 'app.town', record_type: 0, value: '192.168.1.1', ttl: 300, priority: 0 },
    ])
  })

  it('hides admin buttons for non-admin users', async () => {
    mockUseRequireAuth.mockReturnValue({ username: 'user', admin: false })

    renderDNS()
    await waitFor(() => {
      expect(screen.getByText('app.town')).toBeTruthy()
    })
    expect(screen.queryByText('Setup DNS')).toBeNull()
    expect(screen.queryByText('Add Record')).toBeNull()
    expect(screen.queryByText('Change')).toBeNull()

    // Restore admin mock
    mockUseRequireAuth.mockReturnValue({ username: 'admin', admin: true })
  })
})
