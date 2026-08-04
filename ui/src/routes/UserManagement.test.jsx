import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
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
            grants: [],
            networks: [],
          },
          {
            username: 'bob',
            real_name: 'Bob B',
            email: 'bob@test.com',
            phone: '555-0002',
            admin: false,
            disabled: true,
            grants: [],
            networks: [],
          },
          {
            username: 'carol',
            real_name: 'Carol C',
            email: 'carol@test.com',
            phone: '555-0003',
            admin: false,
            disabled: false,
            grants: ['wireguard', 'gfeh'],
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
  // Somebody is always logged in when this screen is on screen, and who they
  // are decides what the edit dialog offers: the account kind and its scope are
  // admin-only fields, so the controls for them are hidden from anybody else.
  // Without a viewer, getAccount() returns null, every viewer reads as a
  // non-admin, and the admin-only assertions below would pass by rendering
  // nothing at all.
  beforeEach(() => {
    localStorage.setItem('town-os-account', JSON.stringify({ username: 'alice', admin: true }))
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('renders role badges for admin and user', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Admin')).toBeTruthy()
    })
    expect(screen.getByText('User')).toBeTruthy()
  })

  it('renders a badge per grant an account holds', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Object storage')).toBeTruthy()
    })
    // One badge per grant held, from the shared GRANTS registry.
    expect(screen.getByText('WireGuard peers')).toBeTruthy()
    const badge = screen.getByText('Object storage')
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

  it('prefills the grant toggles and scope when editing a granted account', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('carol')).toBeTruthy()
    })
    openEditDialog('carol')

    await waitFor(() => {
      expect(screen.getByLabelText('Object storage')).toBeTruthy()
    })
    expect(screen.getByLabelText('Object storage').checked).toBe(true)
    // The scope selector offers office and lab (home filtered out), office ticked.
    expect(screen.getByLabelText('office').checked).toBe(true)
    expect(screen.getByLabelText('lab').checked).toBe(false)
    expect(screen.queryByLabelText('home')).toBeNull()
  })

  it('saves an updated network scope', async () => {
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
    expect(fields.networks.sort()).toEqual(['lab', 'office'])
    expect(fields.admin).toBeUndefined()
  })

  it('rejects enabling a grant with no networks selected', async () => {
    mockUpdateAccount.mockClear()
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('bob')).toBeTruthy()
    })
    // bob is a plain, non-admin account.
    openEditDialog('bob')
    await waitFor(() => {
      expect(screen.getByLabelText('Object storage')).toBeTruthy()
    })
    // Turn a grant on but pick no networks, then save.
    fireEvent.click(screen.getByLabelText('Object storage'))
    fireEvent.click(screen.getByText('Save Changes'))

    // The client is never called; the form is rejected client-side.
    await waitFor(() => {
      expect(screen.getByLabelText('Object storage')).toBeTruthy()
    })
    expect(mockUpdateAccount).not.toHaveBeenCalled()
  })

  // --- Grants ---

  // Changing what kind an account is, is an administrator's decision: the
  // server rejects both `grants` and `networks` from anybody else, so
  // offering the control to a non-admin would be offering a toggle whose every
  // use fails the whole edit.
  it('hides the grant controls from a non-admin viewer', async () => {
    localStorage.setItem('town-os-account', JSON.stringify({ username: 'bob', admin: false }))
    try {
      renderUserManagement()
      await waitFor(() => {
        expect(screen.getByText('bob')).toBeTruthy()
      })
      openEditDialog('bob')
      await waitFor(() => {
        expect(screen.getByLabelText('Email')).toBeTruthy()
      })
      expect(screen.queryByLabelText('Object storage')).toBeNull()
    } finally {
      localStorage.clear()
    }
  })

  // ... and an ordinary edit by that same non-admin still has to work, which is
  // the reason the kind fields are omitted rather than merely disabled: sending
  // them unchanged would 403 a password change.
  it('omits the grant fields from an edit that did not touch them', async () => {
    localStorage.setItem('town-os-account', JSON.stringify({ username: 'bob', admin: false }))
    mockUpdateAccount.mockClear()
    try {
      renderUserManagement()
      await waitFor(() => {
        expect(screen.getByText('bob')).toBeTruthy()
      })
      openEditDialog('bob')
      await waitFor(() => {
        expect(screen.getByLabelText('Email')).toBeTruthy()
      })
      fireEvent.click(screen.getByText('Save Changes'))

      await waitFor(() => {
        expect(mockUpdateAccount).toHaveBeenCalled()
      })
      const [, fields] = mockUpdateAccount.mock.calls[0]
      expect(fields.grants).toBeUndefined()
      expect(fields.networks).toBeUndefined()
    } finally {
      localStorage.clear()
    }
  })

  // An administrator opening a scoped account and saving without touching the
  // scope must not send `networks` either. The stored scope is normalized
  // (deduped and sorted) while the dialog holds it in click order, so comparing
  // the two directly would report a change on every single open.
  it('omits an unchanged scope even when the order differs', async () => {
    mockUpdateAccount.mockClear()
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('carol')).toBeTruthy()
    })
    openEditDialog('carol')
    await waitFor(() => {
      expect(screen.getByLabelText('Object storage')).toBeTruthy()
    })
    fireEvent.click(screen.getByText('Save Changes'))

    await waitFor(() => {
      expect(mockUpdateAccount).toHaveBeenCalled()
    })
    const [, fields] = mockUpdateAccount.mock.calls[0]
    expect(fields.grants).toBeUndefined()
    expect(fields.networks).toBeUndefined()
  })

  // An administrator may not make an administrator network-only: the two are
  // opposite statements about the same account and the server refuses the pair.
  it('does not offer grant controls on an administrator', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeTruthy()
    })
    openEditDialog('alice')
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeTruthy()
    })
    expect(screen.queryByLabelText('Object storage')).toBeNull()
  })

  // The toggles are independent: ticking one grant must not carry the other.
  // They are separate capabilities on the server, and a form that sent both
  // would hand out object storage to somebody granted only peer enrollment.
  it('sends only the grants that were ticked', async () => {
    mockUpdateAccount.mockClear()
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('bob')).toBeTruthy()
    })
    openEditDialog('bob')
    await waitFor(() => {
      expect(screen.getByLabelText('WireGuard peers')).toBeTruthy()
    })

    fireEvent.click(screen.getByLabelText('WireGuard peers'))
    fireEvent.click(screen.getByLabelText('office'))
    fireEvent.click(screen.getByText('Save Changes'))

    await waitFor(() => {
      expect(mockUpdateAccount).toHaveBeenCalled()
    })
    const [, fields] = mockUpdateAccount.mock.calls[0]
    expect(fields.grants).toEqual(['wireguard'])
    expect(fields.networks).toEqual(['office'])
  })

  // Every grant in the shared registry gets a checkbox, so adding one needs no
  // new markup here or in the create form.
  it('renders a checkbox for every grant in the registry', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('bob')).toBeTruthy()
    })
    openEditDialog('bob')
    await waitFor(() => {
      expect(screen.getByLabelText('WireGuard peers')).toBeTruthy()
    })
    expect(screen.getByLabelText('Object storage')).toBeTruthy()
  })
})
