import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

import LinksTab from './LinksTab.jsx'

/**
 * A published link exists to be sent to somebody.
 *
 * The tab used to render the bare token, which is the one form of the thing
 * nobody can act on: it left the operator to work out that the serving name is
 * the partition's *http* view, qualified under that partition's network TLD, on
 * :443, at /f/<token>. The server composes the whole URL now, and these tests
 * pin that the tab renders it as something you can click and copy — and that it
 * does NOT render something clickable when the link would not answer.
 */

const EXPOSURES = [
  { token: 'abc123', path: '/photos/beach.jpg', filename: 'beach.jpg', enabled: true, url: 'https://http.gfeh.home/f/abc123' },
  { token: 'off456', path: '/docs/tax.pdf', filename: 'tax.pdf', enabled: false, url: 'https://http.gfeh.home/f/off456' },
  { token: 'noview7', path: '/misc/x.bin', filename: 'x.bin', enabled: true, url: '' },
]

const mockClient = {
  listGfehExposures: vi.fn(() => Promise.resolve(EXPOSURES)),
  withdrawGfehExposure: vi.fn(() => Promise.resolve()),
}

vi.mock('@/lib/client-instance.js', () => ({
  default: () => mockClient,
}))

beforeEach(() => {
  mockClient.listGfehExposures.mockClear()
  mockClient.listGfehExposures.mockResolvedValue(EXPOSURES)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LinksTab', () => {
  it('renders an enabled link as a followable URL, not a bare token', async () => {
    render(<LinksTab network="home" canManage />)

    const link = await screen.findByRole('link', { name: 'https://http.gfeh.home/f/abc123' })
    expect(link.getAttribute('href')).toBe('https://http.gfeh.home/f/abc123')
    // A published link is meant to be opened by whoever it was sent to, and the
    // dashboard is a different origin from the partition's HTTP view.
    expect(link.getAttribute('target')).toBe('_blank')
  })

  // The port gfeh reports for the http view is a container-side backend port
  // the ingress proxies to. A link carrying it is refused by every client that
  // tries it, so it must never appear.
  it('never renders a backend port in a link', async () => {
    const { container } = render(<LinksTab network="home" canManage />)

    await screen.findByRole('link', { name: /abc123/ })
    expect(container.textContent).not.toContain(':9001')
  })

  // The exposure exists but is disabled, so the URL is what it *would* be.
  // Rendering something clickable that answers 404 is worse than plain text.
  it('does not make a disabled link clickable', async () => {
    render(<LinksTab network="home" canManage />)

    await screen.findByRole('link', { name: /abc123/ })
    expect(screen.queryByRole('link', { name: /off456/ })).toBeNull()
    expect(screen.getByText('https://http.gfeh.home/f/off456')).toBeTruthy()
  })

  // No HTTP view is being served, so nothing answers the token. The token is
  // still shown: it is the row's identity and the handle withdraw uses.
  it('falls back to the token when no URL is served', async () => {
    render(<LinksTab network="home" canManage />)

    await screen.findByRole('link', { name: /abc123/ })
    expect(screen.getByText('noview7')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /noview7/ })).toBeNull()
  })

  it('reports an empty partition rather than an empty table', async () => {
    mockClient.listGfehExposures.mockResolvedValue([])
    render(<LinksTab network="home" canManage />)

    await waitFor(() => {
      expect(screen.getByText(/no published links/i)).toBeTruthy()
    })
  })

  // Publishing a file lists it at the http view's root, where anyone who can
  // resolve the name reads it. This tab is the only screen that shows these
  // links, so it is the only place that can say so before somebody publishes
  // another one believing the link is private to whoever they sent it to.
  it('warns that enabled links are listed publicly', async () => {
    render(<LinksTab network="home" canManage />)

    await screen.findByRole('link', { name: /abc123/ })
    expect(screen.getByText(/listed publicly/i)).toBeTruthy()
  })

  // Nothing is being listed, so the warning would describe exposure that is not
  // happening: a disabled exposure contributes no row to the public index, and
  // an empty URL means no HTTP view is served for it to sit at.
  it('does not warn when nothing is actually being served', async () => {
    mockClient.listGfehExposures.mockResolvedValue([
      { token: 'off456', path: '/docs/tax.pdf', filename: 'tax.pdf', enabled: false, url: 'https://http.gfeh.home/f/off456' },
      { token: 'noview7', path: '/misc/x.bin', filename: 'x.bin', enabled: true, url: '' },
    ])
    render(<LinksTab network="home" canManage />)

    await screen.findByText('noview7')
    expect(screen.queryByText(/listed publicly/i)).toBeNull()
  })

  it('does not warn on an empty partition', async () => {
    mockClient.listGfehExposures.mockResolvedValue([])
    render(<LinksTab network="home" canManage />)

    await waitFor(() => {
      expect(screen.getByText(/no published links/i)).toBeTruthy()
    })
    expect(screen.queryByText(/listed publicly/i)).toBeNull()
  })
})
