import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Login from './Login.jsx'

const mockNavigate = vi.fn()
let mockPingResponse = { accounts: 1, admins: 1, needs_setup: false }
let mockToken = null
let mockSessionUsernameResult = Promise.resolve({ username: 'admin' })
let mockSessionExpired = false

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({
    ping: () => Promise.resolve(mockPingResponse),
    sessionUsername: () => mockSessionUsernameResult,
    authenticate: vi.fn(() =>
      Promise.resolve({ token: 'tok', account: { username: 'admin' } }),
    ),
  }),
}))

const mockToastInfo = vi.fn()
vi.mock('sonner', () => ({
  toast: { info: (...args) => mockToastInfo(...args) },
}))

vi.mock('@/lib/auth.js', () => ({
  getToken: () => mockToken,
  setToken: vi.fn(),
  setAccount: vi.fn(),
  getAndClearSessionExpired: () => mockSessionExpired,
}))

function renderLogin() {
  return render(
    <MemoryRouter>
      <Login />
    </MemoryRouter>,
  )
}

describe('Login', () => {
  beforeEach(() => {
    mockPingResponse = { accounts: 1, admins: 1, needs_setup: false }
    mockToken = null
    mockSessionExpired = false
    mockSessionUsernameResult = Promise.resolve({ username: 'admin' })
    mockNavigate.mockReset()
    mockToastInfo.mockReset()
  })

  it('renders the login form', async () => {
    renderLogin()
    await waitFor(() => {
      expect(screen.getByLabelText('Username')).toBeTruthy()
    })
    expect(screen.getByLabelText('Password')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sign In' })).toBeTruthy()
  })

  it('does not show a register link', async () => {
    renderLogin()
    await waitFor(() => {
      expect(screen.getByLabelText('Username')).toBeTruthy()
    })
    expect(screen.queryByText('Create an account')).toBeNull()
  })

  it('redirects to /register when needs_setup is true', async () => {
    mockPingResponse = { accounts: 0, admins: 0, needs_setup: true }
    renderLogin()

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/register')
    })
  })

  it('redirects to /dashboard when valid token exists', async () => {
    mockToken = 'valid-token'
    renderLogin()

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/dashboard')
    })
  })

  it('has vertical spacing between password field and sign in button', async () => {
    renderLogin()
    await waitFor(() => {
      expect(screen.getByLabelText('Password')).toBeTruthy()
    })
    const button = screen.getByRole('button', { name: 'Sign In' })
    const footer = button.closest('[data-slot="card-footer"]')
    expect(footer).toBeTruthy()
    expect(footer.className).toMatch(/pt-6/)
  })

  it('shows a toast when session expired due to system update', async () => {
    mockSessionExpired = true
    renderLogin()

    await waitFor(() => {
      expect(mockToastInfo).toHaveBeenCalledWith(
        'You have been signed out because the system was updated. Please sign in again.',
      )
    })
  })

  it('does not show session expired toast when flag is not set', async () => {
    mockSessionExpired = false
    renderLogin()

    await waitFor(() => {
      expect(screen.getByLabelText('Username')).toBeTruthy()
    })
    expect(mockToastInfo).not.toHaveBeenCalled()
  })

  it('stays on login when token is invalid', async () => {
    mockToken = 'bad-token'
    mockSessionUsernameResult = Promise.reject(new Error('invalid'))
    renderLogin()

    await waitFor(() => {
      expect(screen.getByLabelText('Username')).toBeTruthy()
    })

    // Should not navigate to dashboard
    expect(mockNavigate).not.toHaveBeenCalledWith('/dashboard')
  })
})
