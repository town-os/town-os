import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import UserManagement from './UserManagement.jsx'

const { mockUpdateAccount } = vi.hoisted(() => ({
  mockUpdateAccount: vi.fn(() => Promise.resolve({})),
}))

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listAccounts: vi.fn(() =>
      Promise.resolve({
        entries: [
          {
            username: 'alice',
            real_name: 'Alice A',
            email: 'alice@test.com',
            phone: '555-0001',
            admin: true,
            disabled: false,
            wireguard: false,
            networks: [],
          },
          {
            username: 'bob',
            real_name: 'Bob B',
            email: 'bob@test.com',
            phone: '555-0002',
            admin: false,
            disabled: true,
            wireguard: false,
            networks: [],
          },
          {
            username: 'carol',
            real_name: 'Carol C',
            email: 'carol@test.com',
            phone: '555-0003',
            admin: false,
            disabled: false,
            wireguard: true,
            networks: ['office'],
          },
        ],
        has_more: false,
        total_pages: 1,
        total_count: 3,
      }),
    ),
    // "home" has no WireGuard transport and must be filtered out of the scope UI.
    listNetworks: vi.fn(() =>
      Promise.resolve([
        { name: 'office', tld: 'office', enabled: true, peer_count: 0 },
        { name: 'lab', tld: 'lab', enabled: true, peer_count: 0 },
        { name: 'home', tld: 'home', enabled: true, peer_count: 0 },
      ]),
    ),
    updateAccount: mockUpdateAccount,
    ping: vi.fn(() => Promise.resolve({ admins: 1 })),
  }),
}))

function renderUserManagement() {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <UserManagement />
      </TooltipProvider>
    </MemoryRouter>,
  )
}

// The Edit column's header is itself the text "Edit", so getAllByText('Edit')[n]
// is off by one against the rows and silently opens the wrong user's dialog.
// Find the row by username and click the button inside it.
function openEditDialog(username) {
  const row = screen.getByText(username).closest('tr')
  fireEvent.click(within(row).getByText('Edit'))
}

describe('UserManagement', () => {
  it('renders role badges for admin and user', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Admin')).toBeTruthy()
    })
    expect(screen.getByText('User')).toBeTruthy()
  })

  it('renders a WireGuard badge for a wireguard account', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('WireGuard')).toBeTruthy()
    })
    const badge = screen.getByText('WireGuard')
    // Role badges are display-only, not inside a tooltip trigger.
    expect(badge.closest('[data-slot="tooltip-trigger"]')).toBeNull()
  })

  it('renders status badges for active and disabled', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Disabled')).toBeTruthy()
    })
    expect(screen.getAllByText('Active').length).toBeGreaterThanOrEqual(1)
  })

  it('wraps status badges in tooltip triggers', async () => {
    const { container } = renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Admin')).toBeTruthy()
    })
    const triggers = container.querySelectorAll('[data-slot="tooltip-trigger"]')
    // One status tooltip per account (role badges are plain).
    expect(triggers.length).toBe(3)
  })

  it('role badges are display-only', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Admin')).toBeTruthy()
    })
    const adminBadge = screen.getByText('Admin')
    const userBadge = screen.getByText('User')
    expect(adminBadge.closest('[data-slot="tooltip-trigger"]')).toBeNull()
    expect(userBadge.closest('[data-slot="tooltip-trigger"]')).toBeNull()
    expect(adminBadge.className).not.toContain('cursor-pointer')
    expect(userBadge.className).not.toContain('cursor-pointer')
  })

  it('shows last-admin warning when disabling the only admin', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getAllByText('Active').length).toBeGreaterThan(0)
    })
    // alice is the only admin; her Active badge opens the confirm dialog.
    fireEvent.click(screen.getAllByText('Active')[0])
    await waitFor(() => {
      expect(screen.getByText(/last enabled admin account/)).toBeTruthy()
    })
  })

  it('renders user data in table rows', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeTruthy()
    })
    expect(screen.getByText('bob')).toBeTruthy()
    expect(screen.getByText('Alice A')).toBeTruthy()
    expect(screen.getByText('Bob B')).toBeTruthy()
  })

  it('renders edit buttons for each user', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeTruthy()
    })
    const editButtons = screen.getAllByText('Edit')
    // Column header "Edit" label + one button per user.
    expect(editButtons.length).toBeGreaterThanOrEqual(3)
  })

  it('prefills the WireGuard toggle and scope when editing a wireguard account', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('carol')).toBeTruthy()
    })
    openEditDialog('carol')

    await waitFor(() => {
      expect(screen.getByLabelText('WireGuard-only account')).toBeTruthy()
    })
    expect(screen.getByLabelText('WireGuard-only account').checked).toBe(true)
    // The scope selector offers office and lab (home filtered out), office ticked.
    expect(screen.getByLabelText('office').checked).toBe(true)
    expect(screen.getByLabelText('lab').checked).toBe(false)
    expect(screen.queryByLabelText('home')).toBeNull()
  })

  it('saves updated wireguard scope', async () => {
    mockUpdateAccount.mockClear()
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('carol')).toBeTruthy()
    })
    openEditDialog('carol')
    await waitFor(() => {
      expect(screen.getByLabelText('lab')).toBeTruthy()
    })
    // Add "lab" to the scope and save.
    fireEvent.click(screen.getByLabelText('lab'))
    fireEvent.click(screen.getByText('Save Changes'))

    await waitFor(() => {
      expect(mockUpdateAccount).toHaveBeenCalled()
    })
    const [username, fields] = mockUpdateAccount.mock.calls[0]
    expect(username).toBe('carol')
    expect(fields.wireguard).toBe(true)
    expect(fields.networks.sort()).toEqual(['lab', 'office'])
    expect(fields.admin).toBeUndefined()
  })

  it('rejects enabling wireguard with no networks selected', async () => {
    mockUpdateAccount.mockClear()
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('bob')).toBeTruthy()
    })
    // bob is a plain, non-admin account.
    openEditDialog('bob')
    await waitFor(() => {
      expect(screen.getByLabelText('WireGuard-only account')).toBeTruthy()
    })
    // Turn WireGuard on but pick no networks, then save.
    fireEvent.click(screen.getByLabelText('WireGuard-only account'))
    fireEvent.click(screen.getByText('Save Changes'))

    // The client is never called; the form is rejected client-side.
    await waitFor(() => {
      expect(screen.getByLabelText('WireGuard-only account')).toBeTruthy()
    })
    expect(mockUpdateAccount).not.toHaveBeenCalled()
  })
})
