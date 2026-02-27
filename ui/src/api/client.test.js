import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { SystemControllerClient, ApiError } from './client.js'

describe('SystemControllerClient', () => {
  /** @type {SystemControllerClient} */
  let client
  let originalFetch

  beforeEach(() => {
    client = new SystemControllerClient('http://localhost:5309')
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  /** @param {any} body @param {number} [status] */
  function mockFetch(body, status = 200) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      status,
      json: () => Promise.resolve(body),
      text: () => Promise.resolve(JSON.stringify(body)),
    })
  }

  function mockFetchEmpty(status = 200) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      status,
      json: () => Promise.resolve(null),
      text: () => Promise.resolve(''),
    })
  }

  describe('ping', () => {
    it('returns PingResponse', async () => {
      const pingData = {
        status: 'ok',
        filesystems: 2,
        repositories: 1,
        packages: 5,
        installed: 3,
        accounts: 1,
        admins: 1,
        units: { total: 10, active: 8, failed: 1 },
      }
      mockFetch(pingData)

      const result = await client.ping()
      expect(result).toEqual(pingData)
      expect(globalThis.fetch).toHaveBeenCalledWith('http://localhost:5309/status/ping', {
        headers: {},
      })
    })
  })

  describe('authenticate', () => {
    it('sends credentials and returns token + account', async () => {
      const authResp = {
        token: 'tok123',
        account: {
          username: 'admin',
          email: 'a@b.com',
          phone: '555',
          real_name: 'Admin',
          admin: true,
          disabled: false,
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        },
      }
      mockFetch(authResp)

      const result = await client.authenticate('admin', 'pass')
      expect(result).toEqual(authResp)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account/authenticate',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: 'admin', password: 'pass' }),
        },
      )
    })
  })

  describe('createFilesystem', () => {
    it('sends correct POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.createFilesystem({ name: 'data', quota: 1024 })
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/storage/create',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: 'data', quota: 1024 }),
        },
      )
    })
  })

  describe('listFilesystems', () => {
    it('returns PageResult', async () => {
      const page = {
        entries: [
          { name: 'data', quota: 1024, state: 'user' },
          { name: 'logs', quota: 512, state: 'user' },
        ],
        has_more: false,
        total_pages: 1,
        total_count: 2,
      }
      mockFetch(page)
      client.setToken('tok')

      const result = await client.listFilesystems('')
      expect(result).toEqual(page)
    })

    it('sends state parameter when provided', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listFilesystems('', 'name', 'asc', 'installed')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/storage',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: '', sort_by: 'name', sort_order: 'asc', state: 'installed' }),
        },
      )
    })

    it('omits state parameter when not provided', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listFilesystems('', 'name', 'asc')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/storage',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: '', sort_by: 'name', sort_order: 'asc' }),
        },
      )
    })

    it('sends limit, offset, and search in POST body', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listFilesystems('', 'name', 'asc', 'user', 20, 40, 'data')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/storage',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: '', sort_by: 'name', sort_order: 'asc', state: 'user', limit: 20, offset: 40, search: 'data' }),
        },
      )
    })
  })

  describe('addRepository', () => {
    it('sends name, url, and credentials', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.addRepository('my-repo', 'https://example.com/repo.git', 'user', 'pass')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/repository/add',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({
            name: 'my-repo',
            url: 'https://example.com/repo.git',
            username: 'user',
            password: 'pass',
          }),
        },
      )
    })

    it('defaults username and password to empty strings', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.addRepository('my-repo', 'https://example.com/repo.git')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/repository/add',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({
            name: 'my-repo',
            url: 'https://example.com/repo.git',
            username: '',
            password: '',
          }),
        },
      )
    })
  })

  describe('listUnits', () => {
    it('returns paginated response', async () => {
      const page = {
        entries: [
          {
            Name: 'nginx.service',
            Description: 'nginx',
            LoadState: 'loaded',
            ActiveState: 'active',
            SubState: 'running',
          },
        ],
        has_more: false,
        total_pages: 1,
      }
      mockFetch(page)
      client.setToken('tok')

      const result = await client.listUnits()
      expect(result).toEqual(page)
    })

    it('sends limit and offset query params', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1 })
      client.setToken('tok')

      await client.listUnits('Name', 'asc', 20, 40)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/units?sort_by=Name&sort_order=asc&limit=20&offset=40',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('sends search query param', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1 })
      client.setToken('tok')

      await client.listUnits('Name', 'asc', 20, 0, 'nginx')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('search=nginx'),
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })
  })

  describe('setUnitStatus', () => {
    it('sends action', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.setUnitStatus('nginx.service', 'restart')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/status',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: 'nginx.service', action: 'restart' }),
        },
      )
    })
  })

  describe('createAccount', () => {
    it('sends full request body', async () => {
      const acct = {
        username: 'bob',
        email: 'bob@example.com',
        phone: '555-1234',
        real_name: 'Bob Smith',
        admin: false,
        disabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }
      mockFetch(acct)
      client.setToken('tok')

      const result = await client.createAccount(
        'bob',
        'secret',
        'bob@example.com',
        '555-1234',
        'Bob Smith',
        false,
      )
      expect(result).toEqual(acct)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account/create',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({
            username: 'bob',
            password: 'secret',
            email: 'bob@example.com',
            phone: '555-1234',
            real_name: 'Bob Smith',
            admin: false,
          }),
        },
      )
    })
  })

  describe('listAuditLog', () => {
    it('sends options and returns page', async () => {
      const page = {
        entries: [
          {
            id: 1,
            account: 'admin',
            action: 'create filesystem',
            path: '/storage/create',
            success: true,
            error: '',
            created_at: '2025-01-01T00:00:00Z',
          },
        ],
        has_more: false,
      }
      mockFetch(page)
      client.setToken('tok')

      const result = await client.listAuditLog({ limit: 50 })
      expect(result).toEqual(page)
      expect(globalThis.fetch).toHaveBeenCalledWith('http://localhost:5309/audit/log', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: 'Bearer tok',
        },
        body: JSON.stringify({ limit: 50 }),
      })
    })
  })

  describe('listAccounts', () => {
    it('sends sort params as query string', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listAccounts('username', 'asc')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account?sort_by=username&sort_order=asc',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('sends limit, offset, and search params', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listAccounts('username', 'asc', 20, 40, 'bob')
      const url = globalThis.fetch.mock.calls[0][0]
      expect(url).toContain('limit=20')
      expect(url).toContain('offset=40')
      expect(url).toContain('search=bob')
    })
  })

  describe('listRepositories', () => {
    it('sends sort, limit, offset, and search params', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listRepositories('name', 'asc', 20, 40, 'my-repo')
      const url = globalThis.fetch.mock.calls[0][0]
      expect(url).toContain('sort_by=name')
      expect(url).toContain('sort_order=asc')
      expect(url).toContain('limit=20')
      expect(url).toContain('offset=40')
      expect(url).toContain('search=my-repo')
    })
  })

  describe('listPackages', () => {
    it('sends sort, limit, offset, and search params', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listPackages('name', 'desc', 10, 20, 'nginx')
      const url = globalThis.fetch.mock.calls[0][0]
      expect(url).toContain('sort_by=name')
      expect(url).toContain('sort_order=desc')
      expect(url).toContain('limit=10')
      expect(url).toContain('offset=20')
      expect(url).toContain('search=nginx')
    })
  })

  describe('listInstalled', () => {
    it('sends sort, limit, offset, and search params', async () => {
      mockFetch({ entries: [], has_more: false, total_pages: 1, total_count: 0 })
      client.setToken('tok')

      await client.listInstalled('name', 'asc', 20, 0, 'redis')
      const url = globalThis.fetch.mock.calls[0][0]
      expect(url).toContain('sort_by=name')
      expect(url).toContain('sort_order=asc')
      expect(url).toContain('limit=20')
      expect(url).toContain('search=redis')
    })
  })

  describe('auth header', () => {
    it('present when token is set', async () => {
      mockFetch([])
      client.setToken('mytoken')

      await client.listUnits()
      const [, opts] = globalThis.fetch.mock.calls[0]
      expect(opts.headers['Authorization']).toBe('Bearer mytoken')
    })

    it('absent for public routes when no token set', async () => {
      mockFetch({ status: 'ok', filesystems: 0, repositories: 0, packages: 0, installed: 0, accounts: 0 })

      await client.ping()
      const [, opts] = globalThis.fetch.mock.calls[0]
      expect(opts.headers['Authorization']).toBeUndefined()
    })
  })

  describe('error handling', () => {
    it('throws ApiError on non-200 status', async () => {
      mockFetchEmpty(500)

      await expect(client.ping()).rejects.toThrow('status 500')
      try {
        await client.ping()
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.status).toBe(500)
        expect(err.path).toBe('/status/ping')
      }
    })

    it('throws ApiError with status 401 for unauthorized', async () => {
      mockFetchEmpty(401)

      try {
        await client.listUnits()
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.status).toBe(401)
      }
    })

    it('throws ApiError on POST failure', async () => {
      mockFetchEmpty(403)

      try {
        await client.disableAccount('bob')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.status).toBe(403)
        expect(err.path).toBe('/account/disable')
      }
    })
  })

  describe('logReplay', () => {
    it('parses SSE data lines into JournalEntry objects', async () => {
      const entry1 = { Message: 'hello', Cursor: 'c1', RealtimeTimestamp: '2025-01-01T00:00:00Z' }
      const entry2 = { Message: 'world', Cursor: 'c2', RealtimeTimestamp: '2025-01-01T00:00:01Z' }
      const sseText = `data: ${JSON.stringify(entry1)}\n\ndata: ${JSON.stringify(entry2)}\n\n`
      const encoder = new TextEncoder()
      const encoded = encoder.encode(sseText)

      let readCount = 0
      const mockReader = {
        read: vi.fn().mockImplementation(() => {
          if (readCount === 0) {
            readCount++
            return Promise.resolve({ done: false, value: encoded })
          }
          return Promise.resolve({ done: true })
        }),
      }

      globalThis.fetch = vi.fn().mockResolvedValue({
        status: 200,
        body: { getReader: () => mockReader },
        text: () => Promise.resolve(''),
      })
      client.setToken('tok')

      const entries = []
      for await (const entry of client.logReplay('nginx.service')) {
        entries.push(entry)
      }
      expect(entries).toEqual([entry1, entry2])
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs?unit=nginx.service',
        { headers: { Authorization: 'Bearer tok' } },
      )
    })
  })

  describe('logTail', () => {
    it('fetches tail entries with correct query params', async () => {
      const result = { entries: [{ Message: 'hello', Cursor: 'c1' }], cursor: 'c1' }
      mockFetch(result)
      client.setToken('tok')

      const data = await client.logTail('nginx.service', 50)
      expect(data).toEqual(result)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=50',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes before cursor when provided', async () => {
      mockFetch({ entries: [], cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, 'cursor-abc')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=100&before=cursor-abc',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes grep parameter when provided', async () => {
      mockFetch({ entries: [], cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 50, undefined, undefined, 'error')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=50&grep=error',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes after cursor when provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, undefined, 'cursor-xyz')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=100&after=cursor-xyz',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes both before cursor and grep when provided', async () => {
      mockFetch({ entries: [], cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, 'cursor-abc', undefined, 'warning')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=100&before=cursor-abc&grep=warning',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes since parameter when provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 200, undefined, undefined, undefined, 1700000000)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=200&since=1700000000',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes since with grep when both provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, undefined, undefined, 'error', 1700000000)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=100&grep=error&since=1700000000',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes until parameter when provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 200, undefined, undefined, undefined, undefined, 1700003600)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=200&until=1700003600',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes both since and until when both provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 200, undefined, undefined, undefined, 1700000000, 1700003600)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=200&since=1700000000&until=1700003600',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes since, until, and grep when all provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, undefined, undefined, 'error', 1700000000, 1700003600)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=100&grep=error&since=1700000000&until=1700003600',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes priority parameter when provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 200, undefined, undefined, undefined, undefined, undefined, 3)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=200&priority=3',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes priority with grep and since when all provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, undefined, undefined, 'error', 1700000000, undefined, 3)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/systemd/logs/tail?unit=nginx.service&lines=100&grep=error&since=1700000000&priority=3',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('omits priority when not provided', async () => {
      mockFetch({ entries: [], cursor: '', end_cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100)
      const url = globalThis.fetch.mock.calls[0][0]
      expect(url).not.toContain('priority')
    })
  })

  describe('removeFilesystem', () => {
    it('sends name in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.removeFilesystem('data')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/storage/remove',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: 'data' }),
        },
      )
    })
  })

  describe('modifyFilesystem', () => {
    it('sends name and filesystem in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.modifyFilesystem('data', { name: 'data', quota: 2048 })
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/storage/modify',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: 'data', filesystem: { name: 'data', quota: 2048 } }),
        },
      )
    })
  })

  describe('removeRepository', () => {
    it('sends name in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.removeRepository('my-repo')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/repository/remove',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: 'my-repo' }),
        },
      )
    })
  })

  describe('refreshRepositories', () => {
    it('sends POST to /repository/refresh', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.refreshRepositories()
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/repository/refresh',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({}),
        },
      )
    })
  })

  describe('getAccount', () => {
    it('sends username and returns account', async () => {
      const acct = {
        username: 'bob',
        email: 'bob@example.com',
        phone: '555',
        real_name: 'Bob',
        admin: false,
        disabled: false,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }
      mockFetch(acct)
      client.setToken('tok')

      const result = await client.getAccount('bob')
      expect(result).toEqual(acct)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ username: 'bob' }),
        },
      )
    })
  })

  describe('disableAccount', () => {
    it('sends username in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.disableAccount('bob')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account/disable',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ username: 'bob' }),
        },
      )
    })
  })

  describe('disablePackage', () => {
    it('sends name in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.disablePackage('core', 'nginx')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/disable',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('enablePackage', () => {
    it('sends name in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.enablePackage('core', 'nginx')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/enable',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('enableAccount', () => {
    it('sends username in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.enableAccount('bob')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account/enable',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ username: 'bob' }),
        },
      )
    })
  })

  describe('ApiError problem detail parsing', () => {
    it('parses RFC 9457 problem detail on error', async () => {
      const problem = {
        type: 'about:blank#500',
        title: 'Internal Server Error',
        status: 500,
        detail: 'btrfs qgroup limit: quota not enabled',
      }
      globalThis.fetch = vi.fn().mockResolvedValue({
        status: 500,
        text: () => Promise.resolve(JSON.stringify(problem)),
      })

      try {
        await client.ping()
        expect.fail('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.problem).toEqual(problem)
        expect(err.detail).toBe('btrfs qgroup limit: quota not enabled')
        expect(err.message).toContain('btrfs qgroup limit: quota not enabled')
        expect(err.message).toContain('GET')
        expect(err.message).toContain('/status/ping')
      }
    })

    it('falls back to raw body when not problem+json', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        status: 502,
        text: () => Promise.resolve('Bad Gateway'),
      })

      try {
        await client.ping()
        expect.fail('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.problem).toBeNull()
        expect(err.detail).toBe('Bad Gateway')
      }
    })

    it('parses legacy echo error format', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        status: 401,
        text: () => Promise.resolve(JSON.stringify({ message: 'missing authorization token' })),
      })

      try {
        await client.ping()
        expect.fail('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.detail).toBe('missing authorization token')
        expect(err.problem).toBeNull()
      }
    })

    it('handles empty error body', async () => {
      mockFetchEmpty(500)

      try {
        await client.ping()
        expect.fail('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.detail).toBe('')
        expect(err.message).toContain('status 500')
      }
    })

    it('message always includes method, path, and detail', async () => {
      const problem = {
        type: 'about:blank#500',
        title: 'Internal Server Error',
        status: 500,
        detail: 'set quota: btrfs qgroup limit: quota not enabled',
      }
      globalThis.fetch = vi.fn().mockResolvedValue({
        status: 500,
        text: () => Promise.resolve(JSON.stringify(problem)),
      })
      client.setToken('tok')

      try {
        await client.createFilesystem({ name: 'test' })
        expect.fail('should have thrown')
      } catch (err) {
        expect(err.message).toBe('POST /storage/create: set quota: btrfs qgroup limit: quota not enabled')
      }
    })

    it('never shows Internal Server Error when detail is present', async () => {
      const problem = {
        type: 'about:blank#500',
        title: 'Internal Server Error',
        status: 500,
        detail: 'actual error message',
      }
      globalThis.fetch = vi.fn().mockResolvedValue({
        status: 500,
        text: () => Promise.resolve(JSON.stringify(problem)),
      })

      try {
        await client.ping()
        expect.fail('should have thrown')
      } catch (err) {
        expect(err.message).not.toContain('Internal Server Error')
        expect(err.message).toContain('actual error message')
      }
    })
  })

  describe('uploadArchive', () => {
    it('sends FormData with auth header and no Content-Type', async () => {
      mockFetch({ needs_restart: true, message: 'archive unpacked successfully' })
      client.setToken('tok')

      const file = new File(['data'], 'backup.tar.gz', { type: 'application/gzip' })
      const result = await client.uploadArchive('my-data', file)
      expect(result).toEqual({ needs_restart: true, message: 'archive unpacked successfully' })

      const [url, opts] = globalThis.fetch.mock.calls[0]
      expect(url).toBe('http://localhost:5309/storage/upload-archive')
      expect(opts.method).toBe('POST')
      expect(opts.headers['Authorization']).toBe('Bearer tok')
      expect(opts.headers['Content-Type']).toBeUndefined()
      expect(opts.body).toBeInstanceOf(FormData)
      expect(opts.body.get('subvolume')).toBe('my-data')
      expect(opts.body.get('archive')).toBeInstanceOf(File)
    })

    it('throws ApiError on non-200', async () => {
      mockFetchEmpty(403)
      client.setToken('tok')

      const file = new File(['data'], 'backup.tar.gz')
      await expect(client.uploadArchive('reserved', file)).rejects.toThrow(ApiError)
    })
  })

  describe('downloadArchive', () => {
    it('sends JSON body with subvolume and returns raw Response', async () => {
      const mockResponse = {
        status: 200,
        json: () => Promise.resolve(null),
        text: () => Promise.resolve(''),
        body: 'mock-stream',
      }
      globalThis.fetch = vi.fn().mockResolvedValue(mockResponse)
      client.setToken('tok')

      const resp = await client.downloadArchive('data')
      expect(resp).toBe(mockResponse)

      const [url, opts] = globalThis.fetch.mock.calls[0]
      expect(url).toBe('http://localhost:5309/storage/download-archive')
      expect(opts.method).toBe('POST')
      expect(opts.headers['Content-Type']).toBe('application/json')
      expect(opts.headers['Authorization']).toBe('Bearer tok')
      expect(JSON.parse(opts.body)).toEqual({ subvolume: 'data' })
    })

    it('includes paths when provided', async () => {
      const mockResponse = {
        status: 200,
        json: () => Promise.resolve(null),
        text: () => Promise.resolve(''),
        body: 'mock-stream',
      }
      globalThis.fetch = vi.fn().mockResolvedValue(mockResponse)
      client.setToken('tok')

      await client.downloadArchive('data', ['config', 'logs'])
      const [, opts] = globalThis.fetch.mock.calls[0]
      expect(JSON.parse(opts.body)).toEqual({ subvolume: 'data', paths: ['config', 'logs'] })
    })

    it('includes stop_service when provided', async () => {
      const mockResponse = {
        status: 200,
        json: () => Promise.resolve(null),
        text: () => Promise.resolve(''),
        body: 'mock-stream',
      }
      globalThis.fetch = vi.fn().mockResolvedValue(mockResponse)
      client.setToken('tok')

      await client.downloadArchive('data', undefined, 'my-app.service')
      const [, opts] = globalThis.fetch.mock.calls[0]
      expect(JSON.parse(opts.body)).toEqual({ subvolume: 'data', stop_service: 'my-app.service' })
    })

    it('includes format when provided', async () => {
      const mockResponse = {
        status: 200,
        json: () => Promise.resolve(null),
        text: () => Promise.resolve(''),
        body: 'mock-stream',
      }
      globalThis.fetch = vi.fn().mockResolvedValue(mockResponse)
      client.setToken('tok')

      await client.downloadArchive('data', undefined, undefined, 'tar.bz2')
      const [, opts] = globalThis.fetch.mock.calls[0]
      expect(JSON.parse(opts.body)).toEqual({ subvolume: 'data', format: 'tar.bz2' })
    })

    it('throws ApiError on non-200', async () => {
      mockFetchEmpty(500)
      client.setToken('tok')

      await expect(client.downloadArchive('data')).rejects.toThrow(ApiError)
    })
  })

  describe('getPackageQuestionsByIdentity', () => {
    it('sends name and version and returns questions', async () => {
      const questions = {
        hostname: { query: 'What hostname?', type: 'hostname' },
        port: { query: 'What port?', type: 'port' },
      }
      mockFetch(questions)
      client.setToken('tok')

      const result = await client.getPackageQuestionsByIdentity('core', 'nginx', '1.0')
      expect(result).toEqual(questions)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/questions/identity',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0' }),
        },
      )
    })
  })

  describe('getResponses', () => {
    it('sends repo, name, and version and returns responses', async () => {
      const responses = { hostname: 'example', port: '8080' }
      mockFetch(responses)
      client.setToken('tok')

      const result = await client.getResponses('core', 'nginx', '1.0')
      expect(result).toEqual(responses)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/responses',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0' }),
        },
      )
    })
  })

  describe('getLastResponses', () => {
    it('sends repo and name and returns responses', async () => {
      const responses = { hostname: 'cached', port: '9090' }
      mockFetch(responses)
      client.setToken('tok')

      const result = await client.getLastResponses('core', 'nginx')
      expect(result).toEqual(responses)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/last-responses',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('clearLastResponses', () => {
    it('sends repo and name', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.clearLastResponses('core', 'nginx')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/clear-last-responses',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('self-authenticated methods', () => {
    it('listSessions uses explicit token', async () => {
      const sessions = [
        { id: 's1', username: 'admin', created_at: '2025-01-01T00:00:00Z', last_used: '2025-01-01T00:00:00Z' },
      ]
      mockFetch(sessions)

      const result = await client.listSessions('explicit-tok')
      expect(result).toEqual(sessions)
      const [, opts] = globalThis.fetch.mock.calls[0]
      expect(opts.headers['Authorization']).toBe('Bearer explicit-tok')
    })

    it('sessionUsername returns username string', async () => {
      mockFetch({ username: 'admin' })

      const result = await client.sessionUsername('explicit-tok')
      expect(result).toBe('admin')
    })

    it('sessionUsername rejects without token', async () => {
      mockFetch(null, 401)

      await expect(client.sessionUsername('')).rejects.toThrow(ApiError)
    })

    it('revokeSession sends session_id', async () => {
      mockFetchEmpty()

      await client.revokeSession('sess-123')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/account/session/revoke',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: 'sess-123' }),
        },
      )
    })
  })

  describe('installPackage', () => {
    it('sends install request', async () => {
      mockFetchEmpty()

      await client.installPackage('core', 'nginx', '1.0', { hostname: 'example' })
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/install',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0', responses: { hostname: 'example' } }),
        },
      )
    })

    it('throws ApiError with validation_errors on 422', async () => {
      const problemBody = {
        type: 'about:blank#422',
        title: 'Unprocessable Entity',
        status: 422,
        detail: '2 response validation error(s)',
        validation_errors: [
          { name: 'hostname', error: 'question has no response' },
          { name: 'port', error: 'empty response' },
        ],
      }
      mockFetch(problemBody, 422)

      try {
        await client.installPackage('core', 'nginx', '1.0', {})
        expect.fail('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.status).toBe(422)
        expect(err.problem).toBeTruthy()
        expect(err.problem.validation_errors).toHaveLength(2)
        expect(err.problem.validation_errors[0].name).toBe('hostname')
        expect(err.problem.validation_errors[1].name).toBe('port')
      }
    })
  })

  describe('uninstallPackage', () => {
    it('sends uninstall request without purge', async () => {
      mockFetchEmpty()

      await client.uninstallPackage('core', 'nginx', '1.0')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/uninstall',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0', purge_volumes: false }),
        },
      )
    })

    it('sends uninstall request with purge', async () => {
      mockFetchEmpty()

      await client.uninstallPackage('core', 'nginx', '1.0', true)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/uninstall',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0', purge_volumes: true }),
        },
      )
    })
  })

  describe('purgeVolumes', () => {
    it('sends purge request', async () => {
      mockFetchEmpty()

      await client.purgeVolumes('core', 'nginx')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/purge-volumes',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('getSettings', () => {
    it('fetches all settings', async () => {
      mockFetch({ default_quota: '0' })

      const result = await client.getSettings()
      expect(result).toEqual({ default_quota: '0' })
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/settings',
        { headers: {} },
      )
    })
  })

  describe('setSetting', () => {
    it('sends set request', async () => {
      mockFetchEmpty()

      await client.setSetting('default_quota', '53687091200')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/settings/set',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ key: 'default_quota', value: '53687091200' }),
        },
      )
    })
  })

  describe('getSetting', () => {
    it('fetches a single setting value', async () => {
      mockFetch({ key: 'default_quota', value: '53687091200' })

      const value = await client.getSetting('default_quota')
      expect(value).toBe('53687091200')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/settings/get',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ key: 'default_quota' }),
        },
      )
    })
  })

  describe('listPackageVersions', () => {
    it('sends name and returns versions', async () => {
      mockFetch(['2.0', '1.0'])
      client.setToken('tok')

      const result = await client.listPackageVersions('nginx')
      expect(result).toEqual(['2.0', '1.0'])
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/versions',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ name: 'nginx' }),
        },
      )
    })
  })

  describe('listUninstalledVolumes', () => {
    it('sends name and returns response object', async () => {
      const resp = {
        has_uninstalled_volumes: true,
        uninstalled_versions: ['1.0'],
        installed_versions: ['2.0'],
      }
      mockFetch(resp)
      client.setToken('tok')

      const result = await client.listUninstalledVolumes('core', 'nginx')
      expect(result).toEqual(resp)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/uninstalled-volumes',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('purgeUninstalledVolumes', () => {
    it('sends name in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.purgeUninstalledVolumes('core', 'nginx')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/purge-uninstalled-volumes',
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: 'Bearer tok',
          },
          body: JSON.stringify({ repo: 'core', name: 'nginx' }),
        },
      )
    })
  })

  describe('installPackage with reuseVolumes', () => {
    it('includes reuse_volumes when true', async () => {
      mockFetchEmpty()

      await client.installPackage('core', 'nginx', '1.0', {}, true)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/install',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0', responses: {}, reuse_volumes: true }),
        },
      )
    })
  })

  describe('installPackage with importFromVersion', () => {
    it('includes import_from_version when set', async () => {
      mockFetchEmpty()

      await client.installPackage('core', 'nginx', '2.0', {}, false, '1.0')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/install',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '2.0', responses: {}, import_from_version: '1.0' }),
        },
      )
    })
  })

  describe('installPackage without optional params', () => {
    it('does not include reuse_volumes or import_from_version', async () => {
      mockFetchEmpty()

      await client.installPackage('core', 'nginx', '1.0', { hostname: 'example' })
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/install',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ repo: 'core', name: 'nginx', version: '1.0', responses: { hostname: 'example' } }),
        },
      )
    })
  })

  describe('listUpgrades', () => {
    it('returns upgrade list', async () => {
      const upgrades = [
        { repo: 'core', name: 'nginx', installed_version: '1.0', latest_version: '2.0', changed: false },
      ]
      mockFetch(upgrades)

      const result = await client.listUpgrades()
      expect(result).toEqual(upgrades)
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/upgrades',
        { headers: {} },
      )
    })
  })

  describe('dismissUpgrades', () => {
    it('sends POST to dismiss endpoint', async () => {
      mockFetchEmpty()

      await client.dismissUpgrades()
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:5309/packages/upgrades/dismiss',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({}),
        },
      )
    })
  })
})
