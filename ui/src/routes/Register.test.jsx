import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Register from './Register.jsx'

const mockNavigate = vi.fn()
let mockPingResponse = { accounts: 0, admins: 0, needs_setup: true }

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

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
    mockPingResponse = { accounts: 0, admins: 0, needs_setup: true }
    mockNavigate.mockReset()
  })

  it('shows bootstrap heading when needs_setup is true', async () => {
    renderRegister()
    await waitFor(() => {
      expect(screen.getByText('Welcome to Town OS')).toBeTruthy()
    })
    expect(
      screen.getByText(
        'Create an administrator account to get started.',
      ),
    ).toBeTruthy()
  })

  it('redirects to login when needs_setup is false', async () => {
    mockPingResponse = { accounts: 1, admins: 1, needs_setup: false }
    renderRegister()

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('renders the create account form', async () => {
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

  it('does not show a sign-in link', async () => {
    renderRegister()
    await waitFor(() => {
      expect(screen.getByText('Welcome to Town OS')).toBeTruthy()
    })
    expect(screen.queryByText('Sign in')).toBeNull()
  })
})
