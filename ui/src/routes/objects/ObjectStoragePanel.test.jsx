import { describe, it, expect, vi, afterEach, beforeAll, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import ObjectStoragePanel from './ObjectStoragePanel.jsx'

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

const ACCOUNTS = [
  { username: 'admin', admin: true },
  { username: 'erik', admin: false, grants: [] },
]

const mockClient = {
  ping: vi.fn(() => Promise.resolve({ username: 'admin', admin: true })),
  listGfehPartitions: vi.fn(() => Promise.resolve(PARTITIONS)),
  listGfehPrincipals: vi.fn(() => Promise.resolve([])),
  listGfehGrants: vi.fn(() => Promise.resolve([])),
  listGfehExposures: vi.fn(() => Promise.resolve([])),
  // `entries`, matching what GET /account actually answers. The mock said
  // `items` and so agreed with a bug in the tab: the real envelope fell through
  // an `|| r` fallback, reached `.filter` as an object, and white-screened the
  // tab on the first poll. A mock that invents its own shape cannot catch that.
  listAccounts: vi.fn(() =>
    Promise.resolve({ entries: ACCOUNTS, has_more: false, total_pages: 1, total_count: ACCOUNTS.length }),
  ),
  // The panel re-reads the caller's own account rather than trusting the copy
  // stored at login, so a kind change made to an open session takes effect
  // without a re-login.
  getAccount: vi.fn((username) =>
    Promise.resolve(ACCOUNTS.find((a) => a.username === username) || null),
  ),
  // Object storage has no switch, so the panel reads no setting: it renders
  // whatever partitions exist. Kept on the mock so an accidental read would
  // fail loudly on the assertion below rather than on an undefined method.
  getSettings: vi.fn(() => Promise.resolve({})),
  setSetting: vi.fn(() => Promise.resolve()),
}

vi.mock('@/lib/client-instance.js', () => ({
  default: () => mockClient,
}))

function renderPanel(initialEntry = '/dashboard/objects', account = { username: 'admin', admin: true }) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <TooltipProvider>
        <ObjectStoragePanel account={account} />
      </TooltipProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  // The tabs below issue authenticated calls through the client, so a session
  // is a precondition for these assertions rather than incidental setup.
  localStorage.setItem('town-os-token', 'test-token')
  localStorage.setItem('town-os-account', JSON.stringify({ username: 'admin', admin: true }))

  mockClient.listGfehPartitions.mockClear()
  mockClient.listGfehPrincipals.mockClear()
  mockClient.listGfehExposures.mockClear()
  mockClient.getAccount.mockClear()
  mockClient.getSettings.mockClear()
  mockClient.setSetting.mockClear()
  mockClient.listGfehPartitions.mockResolvedValue(PARTITIONS)
  mockClient.getSettings.mockResolvedValue({})
  mockClient.getAccount.mockImplementation((username) =>
    Promise.resolve(ACCOUNTS.find((a) => a.username === username) || null),
  )
})

/** Render as somebody other than the seeded admin. */
function renderAs(account, initialEntry) {
  localStorage.setItem('town-os-account', JSON.stringify(account))
  mockClient.getAccount.mockImplementation(() => Promise.resolve(account))
  return renderPanel(initialEntry, account)
}

afterEach(() => {
  localStorage.clear()
})

describe('ObjectStoragePanel', () => {
  it('renders the first partition and its published addresses', async () => {
    renderPanel()

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
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText('smb.gfeh.home')).toBeTruthy()
    })
    expect(screen.getAllByText('Ingress (HTTPS)').length).toBe(2)
    expect(screen.getAllByText('Direct port').length).toBe(1)
  })

  // A partition that exists but is not answering is a distinct state: its data
  // is there, its addresses are not being published.
  it('reports a running partition as running', async () => {
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeTruthy()
    })
  })

  it('shows an unlimited quota rather than a zero', async () => {
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText(/Unlimited/)).toBeTruthy()
    })
  })

  // Object storage being off is a deployment choice, not a failure, so the page
  // says so rather than rendering an error.
  it('explains when object storage is not configured', async () => {
    mockClient.listGfehPartitions.mockResolvedValue([])
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText('Object storage is not configured on this system.')).toBeTruthy()
    })
  })

  // The distinction the whole fix turns on. A partition whose daemon is down
  // arrives as a row with running=false, and the panel must render it as a
  // stopped partition — NOT fall through to "not configured", which is the
  // message for a box that has no object storage at all. The server used to
  // omit a partition it could not start, so a down daemon and an absent feature
  // were the same screen and neither could be acted on.
  it('renders a stopped partition instead of claiming nothing is configured', async () => {
    mockClient.listGfehPartitions.mockResolvedValue([
      { network: 'home', tld: 'home', quota: 0, running: false, names: [] },
    ])
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText('Stopped')).toBeTruthy()
    })
    expect(screen.queryByText('Object storage is not configured on this system.')).toBeNull()
    // ... and the overview says which of the two empty states this is.
    expect(screen.getByText(/daemon is not answering/i)).toBeTruthy()
  })

  it('offers a tab for each concern', async () => {
    renderPanel()

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
    renderPanel('/dashboard/objects?tab=users&network=home')

    await waitFor(() => {
      expect(mockClient.listGfehPrincipals).toHaveBeenCalledWith('home')
    })
  })

  // The account list arrives one poll after first paint, so a tab that mishandles
  // the response shape renders correctly and *then* blanks -- which is what this
  // asserts against. Reading `items` off an `entries` envelope yielded the
  // envelope itself, and filtering an object threw out of render and unmounted
  // the tree. Asserting only on first paint would not have seen it.
  it('survives the account list resolving', async () => {
    renderPanel('/dashboard/objects?tab=users&network=home')

    await waitFor(() => {
      expect(mockClient.listAccounts).toHaveBeenCalled()
    })
    await waitFor(() => {
      expect(screen.getByText('No users have been added to this partition.')).toBeTruthy()
    })
    // Still mounted, and the accounts actually reached the add dialog's candidate list.
    expect(screen.getByText('Add user').closest('button')?.disabled).toBe(false)
  })

  it('scopes the selected partition from the query string', async () => {
    renderPanel('/dashboard/objects?tab=links&network=office')

    await waitFor(() => {
      expect(mockClient.listGfehExposures).toHaveBeenCalledWith('office')
    })
  })

  // The mutating controls are the requests the server would 403. A plain
  // dashboard account still gets the tabs -- reads are open to any
  // authenticated account -- but no buttons.
  it('hides the mutating controls from a plain account', async () => {
    renderAs(
      { username: 'erik', admin: false, grants: [] },
      '/dashboard/objects?tab=users&network=home',
    )

    await waitFor(() => {
      expect(screen.getByText('No users have been added to this partition.')).toBeTruthy()
    })
    expect(screen.queryByText('Add user')).toBeNull()
  })

  // ... and the gfeh grant is what brings them back, without the account
  // being an administrator. The server returns only the partitions such an
  // account is scoped to, so anything selectable here is one it may act on.
  it('shows the mutating controls to an account holding the gfeh grant', async () => {
    renderAs(
      { username: 'erik', admin: false, grants: ['gfeh'], networks: ['home'] },
      '/dashboard/objects?tab=users&network=home',
    )

    await waitFor(() => {
      expect(screen.getByText('Add user')).toBeTruthy()
    })
  })

  // The grant is added by an administrator, on a session that is already
  // open. Reading it from the freshly fetched account rather than the
  // login-time copy is what spares the operator a log out and back in.
  it('picks up the gfeh grant granted after login without a re-login', async () => {
    const stale = { username: 'erik', admin: false, grants: [] }
    localStorage.setItem('town-os-account', JSON.stringify(stale))
    mockClient.getAccount.mockImplementation(() =>
      Promise.resolve({ username: 'erik', admin: false, grants: ['gfeh'], networks: ['home'] }),
    )
    renderPanel('/dashboard/objects?tab=users&network=home', stale)

    await waitFor(() => {
      expect(screen.getByText('Add user')).toBeTruthy()
    })
  })

  // Object storage has no on/off switch any more: it runs the way DNS and the
  // ingress run. The panel must not read a setting to decide whether to render,
  // because a box upgraded from the release that had the switch still has the
  // row -- quite possibly saying false -- and honouring it would blank a panel
  // whose daemons are up.
  it('renders without consulting any setting', async () => {
    mockClient.getSettings.mockResolvedValue({ object_storage_enabled: 'false' })
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText('s3.gfeh.home')).toBeTruthy()
    })
    expect(mockClient.getSettings).not.toHaveBeenCalled()
    expect(mockClient.setSetting).not.toHaveBeenCalled()
  })

  // No partitions is a deployment shape -- a box built without object storage,
  // or one whose partitions are not provisioned -- and must not be reported as
  // "switched off", since there is no longer anything to switch.
  it('explains an empty partition list without offering a switch', async () => {
    mockClient.listGfehPartitions.mockResolvedValue([])
    renderPanel()

    await waitFor(() => {
      expect(screen.getByText('Object storage is not configured on this system.')).toBeTruthy()
    })
    expect(screen.queryByText(/switched off/i)).toBeNull()
    expect(screen.queryByText(/Turn on/i)).toBeNull()
  })
})
