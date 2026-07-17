import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TooltipProvider } from '@/components/ui/tooltip'
import ConnectedPeersPanel from './ConnectedPeersPanel.jsx'

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
})

const { mockRemoveNetworkPeer, mockListConnectedPeers, mockToastSuccess, mockToastError } =
  vi.hoisted(() => ({
    mockRemoveNetworkPeer: vi.fn(() => Promise.resolve()),
    mockListConnectedPeers: vi.fn(),
    mockToastSuccess: vi.fn(),
    mockToastError: vi.fn(),
  }))

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listConnectedPeers: mockListConnectedPeers,
    removeNetworkPeer: mockRemoveNetworkPeer,
  }),
}))

vi.mock('sonner', () => ({
  toast: {
    success: (...args) => mockToastSuccess(...args),
    error: (...args) => mockToastError(...args),
  },
}))

const nowISO = new Date().toISOString()
const longAgoISO = new Date(Date.now() - 26 * 60 * 60 * 1000).toISOString()

const laptop = {
  network: 'office',
  tld: 'office',
  interface: 'townc3d4',
  public_key: 'cGVlck9uZVB1YmxpY0tleQ==',
  name: 'laptop',
  account: 'alice',
  allowed_ip: '10.90.12.2/32',
  endpoint: '203.0.113.9:48123',
  rolodex: false,
  connected: true,
  last_handshake: nowISO,
  rx_bytes: 4096,
  tx_bytes: 2048,
  created_at: nowISO,
}

const phone = {
  network: 'office',
  tld: 'office',
  interface: 'townc3d4',
  public_key: 'cGVlclR3b1B1YmxpY0tleQ==',
  name: 'phone',
  account: 'bob',
  allowed_ip: '10.90.12.3/32',
  endpoint: '',
  rolodex: false,
  connected: false,
  rx_bytes: 0,
  tx_bytes: 0,
  expires_at: new Date(Date.now() + 90 * 60 * 1000).toISOString(),
  created_at: longAgoISO,
}

function renderPanel(isAdmin = true) {
  return render(
    <TooltipProvider>
      <ConnectedPeersPanel isAdmin={isAdmin} />
    </TooltipProvider>,
  )
}

// Find the table row containing the given text, so per-row assertions can't
// accidentally match another peer's cells.
function rowFor(text) {
  return screen.getByText(text).closest('tr')
}

beforeEach(() => {
  vi.clearAllMocks()
  mockListConnectedPeers.mockResolvedValue([laptop, phone])
  mockRemoveNetworkPeer.mockResolvedValue(undefined)
})

describe('ConnectedPeersPanel', () => {
  it('itemizes each peer with its account, overlay IP and endpoint', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())

    const laptopRow = rowFor('laptop')
    expect(within(laptopRow).getByText('alice')).toBeTruthy()
    expect(within(laptopRow).getByText('10.90.12.2/32')).toBeTruthy()
    expect(within(laptopRow).getByText('203.0.113.9:48123')).toBeTruthy()
    expect(within(laptopRow).getByText('cGVlck9uZVB1YmxpY0tleQ==')).toBeTruthy()

    const phoneRow = rowFor('phone')
    expect(within(phoneRow).getByText('bob')).toBeTruthy()
    expect(within(phoneRow).getByText('10.90.12.3/32')).toBeTruthy()
  })

  it('shows the network and its TLD per peer', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())
    const laptopRow = rowFor('laptop')
    expect(within(laptopRow).getByText('office')).toBeTruthy()
    expect(within(laptopRow).getByText('.office')).toBeTruthy()
  })

  it('distinguishes a connected peer from an idle one', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())

    expect(within(rowFor('laptop')).getByText('Connected')).toBeTruthy()
    expect(within(rowFor('phone')).getByText('Idle')).toBeTruthy()
  })

  // "never handshook" and "handshook long ago" are different facts; the panel
  // must not render a never-connected device as though it merely went quiet.
  it('labels a peer that has never handshaken as never connected', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('phone')).toBeTruthy())
    expect(within(rowFor('phone')).getByText('Never connected')).toBeTruthy()
  })

  it('renders transfer counters', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())
    const laptopRow = rowFor('laptop')
    expect(within(laptopRow).getByText('↓ 4.0 KB')).toBeTruthy()
    expect(within(laptopRow).getByText('↑ 2.0 KB')).toBeTruthy()
  })

  it('reports a TTL-bearing enrollment as expiring and a permanent one as never', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())
    expect(within(rowFor('laptop')).getByText('Never')).toBeTruthy()
    expect(within(rowFor('phone')).getByText(/^in \d+m$/)).toBeTruthy()
  })

  it('falls back to a placeholder when a peer has no observed endpoint', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('phone')).toBeTruthy())
    expect(within(rowFor('phone')).getByText('—')).toBeTruthy()
  })

  it('names the enrolling account, or System when there is none', async () => {
    mockListConnectedPeers.mockResolvedValue([{ ...laptop, account: '' }])
    renderPanel()
    await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())
    expect(within(rowFor('laptop')).getByText('System')).toBeTruthy()
  })

  it('renders an empty state when no peers are enrolled', async () => {
    mockListConnectedPeers.mockResolvedValue([])
    renderPanel()
    await waitFor(() =>
      expect(
        screen.getByText('No peers are enrolled on any WireGuard network.'),
      ).toBeTruthy(),
    )
  })

  it('survives the endpoint failing rather than blanking the page', async () => {
    mockListConnectedPeers.mockRejectedValue(new Error('boom'))
    renderPanel()
    await waitFor(() =>
      expect(
        screen.getByText('No peers are enrolled on any WireGuard network.'),
      ).toBeTruthy(),
    )
  })

  describe('disconnect', () => {
    it('confirms before disconnecting and names the peer and network', async () => {
      const user = userEvent.setup()
      renderPanel()
      await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())

      await user.click(within(rowFor('laptop')).getByRole('button', { name: 'Disconnect' }))

      expect(await screen.findByText(/Disconnect "laptop" from "office"\?/)).toBeTruthy()
      // Nothing is torn down until the operator confirms.
      expect(mockRemoveNetworkPeer).not.toHaveBeenCalled()
    })

    it('revokes the peer on the right network when confirmed', async () => {
      const user = userEvent.setup()
      renderPanel()
      await waitFor(() => expect(screen.getByText('phone')).toBeTruthy())

      await user.click(within(rowFor('phone')).getByRole('button', { name: 'Disconnect' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }))

      await waitFor(() =>
        expect(mockRemoveNetworkPeer).toHaveBeenCalledWith('office', 'cGVlclR3b1B1YmxpY0tleQ=='),
      )
      await waitFor(() =>
        expect(mockToastSuccess).toHaveBeenCalledWith('Peer "phone" disconnected'),
      )
    })

    it('does not disconnect when the operator cancels', async () => {
      const user = userEvent.setup()
      renderPanel()
      await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())

      await user.click(within(rowFor('laptop')).getByRole('button', { name: 'Disconnect' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      expect(mockRemoveNetworkPeer).not.toHaveBeenCalled()
    })

    it('surfaces the server problem detail when the disconnect fails', async () => {
      const user = userEvent.setup()
      const err = new Error('request failed')
      err.detail = 'peer not found'
      mockRemoveNetworkPeer.mockRejectedValue(err)

      renderPanel()
      await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())
      await user.click(within(rowFor('laptop')).getByRole('button', { name: 'Disconnect' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }))

      await waitFor(() => expect(mockToastError).toHaveBeenCalledWith('peer not found'))
      expect(mockToastSuccess).not.toHaveBeenCalled()
    })

    it('disables the disconnect action for a non-admin', async () => {
      renderPanel(false)
      await waitFor(() => expect(screen.getByText('laptop')).toBeTruthy())
      expect(within(rowFor('laptop')).getByRole('button', { name: 'Disconnect' }).disabled).toBe(true)
    })
  })
})
