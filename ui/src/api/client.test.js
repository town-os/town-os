import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { SystemControllerClient, ApiError } from './client.js'

describe('SystemControllerClient', () => {
  /** @type {SystemControllerClient} */
  let client
  let originalFetch

  beforeEach(() => {
    client = new SystemControllerClient('http://localhost:8080')
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
        units: { total: 10, active: 8, failed: 1 },
      }
      mockFetch(pingData)

      const result = await client.ping()
      expect(result).toEqual(pingData)
      expect(globalThis.fetch).toHaveBeenCalledWith('http://localhost:8080/status/ping', {
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
        'http://localhost:8080/account/authenticate',
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
        'http://localhost:8080/storage/create',
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
    it('returns parsed array', async () => {
      const fsList = [
        { name: 'data', quota: 1024 },
        { name: 'logs', quota: 512 },
      ]
      mockFetch(fsList)
      client.setToken('tok')

      const result = await client.listFilesystems('')
      expect(result).toEqual(fsList)
    })
  })

  describe('addRepository', () => {
    it('sends name, url, and credentials', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.addRepository('my-repo', 'https://example.com/repo.git', 'user', 'pass')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/repository/add',
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
        'http://localhost:8080/repository/add',
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
    it('returns parsed array', async () => {
      const units = [
        {
          Name: 'nginx.service',
          Description: 'nginx',
          LoadState: 'loaded',
          ActiveState: 'active',
          SubState: 'running',
        },
      ]
      mockFetch(units)
      client.setToken('tok')

      const result = await client.listUnits()
      expect(result).toEqual(units)
    })
  })

  describe('setUnitStatus', () => {
    it('sends action', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.setUnitStatus('nginx.service', 'restart')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/systemd/status',
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
        'http://localhost:8080/account/create',
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
      expect(globalThis.fetch).toHaveBeenCalledWith('http://localhost:8080/audit/log', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: 'Bearer tok',
        },
        body: JSON.stringify({ limit: 50 }),
      })
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
        'http://localhost:8080/systemd/logs?unit=nginx.service',
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
        'http://localhost:8080/systemd/logs/tail?unit=nginx.service&lines=50',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes before cursor when provided', async () => {
      mockFetch({ entries: [], cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, 'cursor-abc')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/systemd/logs/tail?unit=nginx.service&lines=100&before=cursor-abc',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes grep parameter when provided', async () => {
      mockFetch({ entries: [], cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 50, undefined, 'error')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/systemd/logs/tail?unit=nginx.service&lines=50&grep=error',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })

    it('includes both before cursor and grep when provided', async () => {
      mockFetch({ entries: [], cursor: '' })
      client.setToken('tok')

      await client.logTail('nginx.service', 100, 'cursor-abc', 'warning')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/systemd/logs/tail?unit=nginx.service&lines=100&before=cursor-abc&grep=warning',
        expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer tok' }) }),
      )
    })
  })

  describe('removeFilesystem', () => {
    it('sends name in POST body', async () => {
      mockFetchEmpty()
      client.setToken('tok')

      await client.removeFilesystem('data')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/storage/remove',
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
        'http://localhost:8080/storage/modify',
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
        'http://localhost:8080/repository/remove',
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
        'http://localhost:8080/repository/refresh',
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
        'http://localhost:8080/account',
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
        'http://localhost:8080/account/disable',
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

      await expect(client.sessionUsername('')).rejects.toThrow('status 401')
    })

    it('revokeSession sends session_id', async () => {
      mockFetchEmpty()

      await client.revokeSession('sess-123')
      expect(globalThis.fetch).toHaveBeenCalledWith(
        'http://localhost:8080/account/session/revoke',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: 'sess-123' }),
        },
      )
    })
  })
})
