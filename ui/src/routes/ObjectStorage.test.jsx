import { describe, it, expect, vi, afterEach, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import ObjectStorage from './ObjectStorage.jsx'

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
})

const PARTITIONS = [
  {
    network: 'home',
    tld: 'home',
    quota: 0,
    running: true,
    names: [{ view: 's3', fqdn: 's3.gfeh.home', port: 9000, http: true }],
  },
  { network: 'office', tld: 'office', quota: 1073741824, running: false, names: [] },
]

const mockClient = {
  // The page calls useRequireAuth, whose session check pings on mount and logs
  // out when the response carries no username. The panel takes its account as a
  // prop and never reaches this, which is why the panel's own tests do not need
  // it -- and why leaving it off here failed every test in the file on an
  // effect rather than on an assertion.
  ping: vi.fn(() => Promise.resolve({ username: 'admin', admin: true })),
  listGfehPartitions: vi.fn(() => Promise.resolve(PARTITIONS)),
  listGfehPrincipals: vi.fn(() => Promise.resolve([])),
  listGfehGrants: vi.fn(() => Promise.resolve([])),
  listGfehExposures: vi.fn(() => Promise.resolve([])),
  listAccounts: vi.fn(() =>
    Promise.resolve({ entries: [], has_more: false, total_pages: 1, total_count: 0 }),
  ),
  getAccount: vi.fn(() => Promise.resolve({ username: 'admin', admin: true })),
}

vi.mock('@/lib/client-instance.js', () => ({
  default: () => mockClient,
}))

function renderPage(initialEntry = '/dashboard/objects') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <TooltipProvider>
        <ObjectStorage />
      </TooltipProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  // useRequireAuth redirects to "/" without a token, and that navigation drops
  // the query string -- so a page whose partition and tab live in the URL would
  // render its defaults instead of what the test asked for. A session is a
  // precondition for these assertions, not incidental setup.
  localStorage.setItem('town-os-token', 'test-token')
  localStorage.setItem('town-os-account', JSON.stringify({ username: 'admin', admin: true }))
  mockClient.listGfehPartitions.mockClear()
  mockClient.listGfehPrincipals.mockClear()
  mockClient.listGfehExposures.mockClear()
  mockClient.listGfehPartitions.mockResolvedValue(PARTITIONS)
})

afterEach(() => {
  localStorage.clear()
})

// The screen at /dashboard/objects is reachable in its own right, not only as a
// section of the services screen. It has its own nav entry, and a bookmark to
// it has to keep working.
describe('ObjectStorage page', () => {
  it('renders the partition and its published addresses', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('s3.gfeh.home')).toBeTruthy()
    })
  })

  it('sets the page title', async () => {
    renderPage()

    await waitFor(() => {
      expect(document.title).toBe('Object Storage - Town OS')
    })
  })

  it('offers a tab for each concern', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Overview')).toBeTruthy()
    })
    for (const tab of ['Users', 'Grants', 'Links']) {
      expect(screen.getByText(tab)).toBeTruthy()
    }
  })

  // This page keeps `?tab=`, the panel's default. Links written against it
  // predate the services-screen section and must not have been re-keyed by it.
  it('opens the sub-tab named by ?tab=', async () => {
    renderPage('/dashboard/objects?tab=users&network=home')

    await waitFor(() => {
      expect(mockClient.listGfehPrincipals).toHaveBeenCalledWith('home')
    })
  })

  it('scopes the selected partition from the query string', async () => {
    renderPage('/dashboard/objects?tab=links&network=office')

    await waitFor(() => {
      expect(mockClient.listGfehExposures).toHaveBeenCalledWith('office')
    })
  })
})
