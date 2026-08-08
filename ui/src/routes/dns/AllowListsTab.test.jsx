import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockListAllowlist = vi.fn()
const mockAddAllowlist = vi.fn(() => Promise.resolve())
const mockRemoveAllowlist = vi.fn(() => Promise.resolve())

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    listDNSBLAllowlist: mockListAllowlist,
    addDNSBLAllowlist: mockAddAllowlist,
    removeDNSBLAllowlist: mockRemoveAllowlist,
  }),
}))

import AllowListsTab from './AllowListsTab.jsx'

function renderTab(isAdmin = true) {
  return render(
    <MemoryRouter>
      <AllowListsTab isAdmin={isAdmin} />
    </MemoryRouter>,
  )
}

describe('AllowListsTab', () => {
  beforeEach(() => {
    mockListAllowlist.mockReset()
    mockAddAllowlist.mockClear()
    mockRemoveAllowlist.mockClear()

    mockListAllowlist.mockResolvedValue([
      { name: 'cdn.example.com', reason: 'false positive' },
    ])
  })

  it('renders the allowlist entries', async () => {
    renderTab()
    await waitFor(() => {
      expect(screen.getByText('cdn.example.com')).toBeTruthy()
      expect(screen.getByText('false positive')).toBeTruthy()
    })
  })

  it('adds an entry with the name and reason it was given', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('Add entry')).toBeTruthy())

    fireEvent.click(screen.getByText('Add entry'))
    await waitFor(() => expect(screen.getByLabelText('Domain')).toBeTruthy())

    fireEvent.change(screen.getByLabelText('Domain'), {
      target: { value: 'mail.example.net' },
    })
    fireEvent.change(screen.getByLabelText('Reason (optional)'), {
      target: { value: 'needed by the mail client' },
    })

    // Both the trigger and the dialog's submit read "Add entry"; the submit is
    // the one mounted last.
    const submitBtns = screen.getAllByRole('button').filter((b) => b.textContent === 'Add entry')
    fireEvent.click(submitBtns[submitBtns.length - 1])

    await waitFor(() => {
      expect(mockAddAllowlist).toHaveBeenCalledWith('mail.example.net', 'needed by the mail client')
    })
  })

  it('removes an entry only after the confirmation is accepted', async () => {
    renderTab()
    await waitFor(() => expect(screen.getByText('cdn.example.com')).toBeTruthy())

    const buttons = screen.getAllByRole('button')
    fireEvent.click(buttons[buttons.length - 1])

    await waitFor(() => expect(screen.getByText('Remove allowlist entry')).toBeTruthy())
    // Opening the confirmation must not itself remove anything.
    expect(mockRemoveAllowlist).not.toHaveBeenCalled()

    fireEvent.click(screen.getByText('Remove'))
    await waitFor(() => {
      expect(mockRemoveAllowlist).toHaveBeenCalledWith('cdn.example.com')
    })
  })

  it('hides the mutating controls for non-admins but still lists entries', async () => {
    renderTab(false)
    await waitFor(() => expect(screen.getByText('cdn.example.com')).toBeTruthy())
    expect(screen.queryByText('Add entry')).toBeNull()
    expect(screen.queryByText('Actions')).toBeNull()
  })
})
