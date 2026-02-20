import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import UserManagement from './UserManagement.jsx'

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listAccounts: vi.fn(() =>
      Promise.resolve([
        {
          username: 'alice',
          real_name: 'Alice A',
          email: 'alice@test.com',
          phone: '555-0001',
          admin: true,
          disabled: false,
        },
        {
          username: 'bob',
          real_name: 'Bob B',
          email: 'bob@test.com',
          phone: '555-0002',
          admin: false,
          disabled: true,
        },
      ]),
    ),
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

describe('UserManagement', () => {
  it('renders role badges for admin and user', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Admin')).toBeTruthy()
    })
    expect(screen.getByText('User')).toBeTruthy()
  })

  it('renders status badges for active and disabled', async () => {
    renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Active')).toBeTruthy()
    })
    expect(screen.getByText('Disabled')).toBeTruthy()
  })

  it('wraps role badges in tooltip triggers', async () => {
    const { container } = renderUserManagement()
    await waitFor(() => {
      expect(screen.getByText('Admin')).toBeTruthy()
    })
    const triggers = container.querySelectorAll('[data-slot="tooltip-trigger"]')
    // 2 role tooltips + 2 status tooltips = 4 total
    expect(triggers.length).toBe(4)
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
    // Column header "Edit" label + 2 edit buttons
    expect(editButtons.length).toBeGreaterThanOrEqual(2)
  })
})
