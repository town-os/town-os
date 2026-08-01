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
    names: [
      { view: 's3', fqdn: 's3.gfeh.home', port: 9000, http: true },
      { view: 'http', fqdn: 'http.gfeh.home', port: 9001, http: true },
      { view: 'smb', fqdn: 'smb.gfeh.home', port: 4450, http: false },
    ],
  },
  {
    network: 'office',
    tld: 'office',
    quota: 1073741824,
    running: false,
    names: [],
  },
]

const mockClient = {
  ping: vi.fn(() => Promise.resolve({ username: 'admin', admin: true })),
  listGfehPartitions: vi.fn(() => Promise.resolve(PARTITIONS)),
  listGfehPrincipals: vi.fn(() => Promise.resolve([])),
  listGfehGrants: vi.fn(() => Promise.resolve([])),
  listGfehExposures: vi.fn(() => Promise.resolve([])),
  listAccounts: vi.fn(() => Promise.resolve({ items: [] })),
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
  // useRequireAuth redirects to "/" when there is no token, and that navigation
  // drops the query string -- so a page whose selected partition and tab live
  // in the URL renders its defaults instead of what the test asked for. A
  // session is a precondition for these assertions, not incidental setup.
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

describe('ObjectStorage', () => {
  it('renders the first partition and its published addresses', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('s3.gfeh.home')).toBeTruthy()
    })
    expect(screen.getByText('http.gfeh.home')).toBeTruthy()
  })

  // Four of the five views sit behind the ingress on :443 and are browsed to;
  // SMB is not HTTP and is dialled on its own port. Telling somebody to browse
  // to an SMB address hands them a connection that completes and then does
  // nothing.
  it('distinguishes an ingress-fronted view from a direct port', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('smb.gfeh.home')).toBeTruthy()
    })
    expect(screen.getAllByText('Ingress (HTTPS)').length).toBe(2)
    expect(screen.getAllByText('Direct port').length).toBe(1)
  })

  // A partition that exists but is not answering is a distinct state: its data
  // is there, its addresses are not being published.
  it('reports a running partition as running', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeTruthy()
    })
  })

  it('shows an unlimited quota rather than a zero', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText(/Unlimited/)).toBeTruthy()
    })
  })

  // Object storage being off is a deployment choice, not a failure, so the page
  // says so rather than rendering an error.
  it('explains when object storage is not configured', async () => {
    mockClient.listGfehPartitions.mockResolvedValue([])
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Object storage is not configured on this system.')).toBeTruthy()
    })
  })

  it('offers a tab for each concern', async () => {
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Overview')).toBeTruthy()
    })
    expect(screen.getByText('Users')).toBeTruthy()
    expect(screen.getByText('Grants')).toBeTruthy()
    expect(screen.getByText('Links')).toBeTruthy()
  })

  // The tab and the partition both live in the URL, so a link to one
  // partition's grants is a link somebody can send.
  it('opens the tab named in the query string', async () => {
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

  it('sets the page title', async () => {
    renderPage()

    await waitFor(() => {
      expect(document.title).toBe('Object Storage - Town OS')
    })
  })
})
