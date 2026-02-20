import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Register from './Register.jsx'

let mockPingResponse = { accounts: 0 }

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    ping: () => Promise.resolve(mockPingResponse),
    createAccount: vi.fn(() => Promise.resolve({})),
    authenticate: vi.fn(() =>
      Promise.resolve({ token: 'tok', account: { username: 'admin' } }),
    ),
  }),
}))

vi.mock('@/lib/auth.js', () => ({
  getToken: () => null,
  setToken: vi.fn(),
  setAccount: vi.fn(),
}))

function renderRegister() {
  return render(
    <MemoryRouter>
      <Register />
    </MemoryRouter>,
  )
}

describe('Register', () => {
  beforeEach(() => {
    mockPingResponse = { accounts: 0 }
  })

  it('shows bootstrap heading when no accounts exist', async () => {
    renderRegister()
    await waitFor(() => {
      expect(screen.getByText('Welcome to town-os')).toBeTruthy()
    })
    expect(
      screen.getByText(
        'No accounts exist yet. Create an administrator account to get started.',
      ),
    ).toBeTruthy()
  })

  it('does not show bootstrap heading when accounts exist', async () => {
    mockPingResponse = { accounts: 1 }
    renderRegister()

    // Wait for the ping to resolve
    await waitFor(() => {
      expect(screen.getByLabelText('Username')).toBeTruthy()
    })

    expect(screen.queryByText('Welcome to town-os')).toBeNull()
  })

  it('always renders the create account form', async () => {
    renderRegister()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Create Account' })).toBeTruthy()
    })
    expect(screen.getByLabelText('Username')).toBeTruthy()
    expect(screen.getByLabelText('Password')).toBeTruthy()
    expect(screen.getByLabelText('Confirm Password')).toBeTruthy()
    expect(screen.getByLabelText('Real Name')).toBeTruthy()
    expect(screen.getByLabelText('Phone')).toBeTruthy()
    expect(screen.getByLabelText('Email')).toBeTruthy()
  })
})
