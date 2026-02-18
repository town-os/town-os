import { describe, it, expect, beforeAll } from 'vitest'
import { SystemControllerClient } from './client.js'

const baseURL = process.env.INTEGRATION_URL
if (!baseURL) {
  throw new Error('INTEGRATION_URL environment variable is required')
}

describe('SystemControllerClient integration', () => {
  /** @type {SystemControllerClient} */
  let client

  beforeAll(() => {
    client = new SystemControllerClient(baseURL)
  })

  // --- Public endpoints ---

  describe('ping', () => {
    it('returns ok status', async () => {
      const resp = await client.ping()
      expect(resp.status).toBe('ok')
      expect(typeof resp.filesystems).toBe('number')
      expect(typeof resp.repositories).toBe('number')
      expect(typeof resp.packages).toBe('number')
      expect(typeof resp.installed).toBe('number')
      expect(typeof resp.accounts).toBe('number')
    })

    it('includes unit counts from systemd', async () => {
      const resp = await client.ping()
      expect(resp.units).toBeDefined()
      expect(resp.units.total).toBeGreaterThan(0)
      expect(resp.units.active).toBeGreaterThan(0)
    })
  })

  // --- Account lifecycle ---

  describe('account lifecycle', () => {
    it('creates first account without auth (bootstrap)', async () => {
      const acct = await client.createAccount(
        'admin',
        'adminpass',
        'admin@test.com',
        '555-0001',
        'Test Admin',
        true,
      )
      expect(acct.username).toBe('admin')
      expect(acct.admin).toBe(true)
      expect(acct.email).toBe('admin@test.com')
    })

    it('authenticates and gets token', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      expect(resp.token).toBeTruthy()
      expect(resp.account.username).toBe('admin')
      client.setToken(resp.token)
    })

    it('gets account by username', async () => {
      const acct = await client.getAccount('admin')
      expect(acct.username).toBe('admin')
      expect(acct.real_name).toBe('Test Admin')
    })

    it('updates account fields', async () => {
      const acct = await client.updateAccount('admin', {
        real_name: 'Updated Admin',
      })
      expect(acct.real_name).toBe('Updated Admin')
    })

    it('lists accounts', async () => {
      const accounts = await client.listAccounts()
      expect(accounts.length).toBeGreaterThanOrEqual(1)
      expect(accounts.some((a) => a.username === 'admin')).toBe(true)
    })

    it('creates a second account (requires auth)', async () => {
      const acct = await client.createAccount(
        'user1',
        'userpass',
        'user1@test.com',
        '555-0002',
        'Regular User',
        false,
      )
      expect(acct.username).toBe('user1')
      expect(acct.admin).toBe(false)
    })

    it('disables an account', async () => {
      await client.disableAccount('user1')
      const acct = await client.getAccount('user1')
      expect(acct.disabled).toBe(true)
    })
  })

  // --- Session management ---

  describe('sessions', () => {
    /** @type {string} */
    let token

    beforeAll(async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      token = resp.token
      client.setToken(token)
    })

    it('lists sessions for token', async () => {
      const sessions = await client.listSessions(token)
      expect(Array.isArray(sessions)).toBe(true)
      expect(sessions.length).toBeGreaterThanOrEqual(1)
      expect(sessions[0].username).toBe('admin')
    })

    it('gets session username', async () => {
      const username = await client.sessionUsername(token)
      expect(username).toBe('admin')
    })

    it('creates and revokes a session', async () => {
      await client.authenticate('admin', 'adminpass')

      const before = await client.listSessions(token)
      await client.revokeSession(before[before.length - 1].id)
      const after = await client.listSessions(token)
      expect(after.length).toBe(before.length - 1)
    })
  })

  // --- Systemd ---

  describe('systemd', () => {
    beforeAll(async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
    })

    it('lists units from real systemd', async () => {
      const units = await client.listUnits()
      expect(units.length).toBeGreaterThan(0)
      const testserver = units.find(
        (u) => u.Name === 'town-os-testserver.service',
      )
      expect(testserver).toBeDefined()
      expect(testserver.ActiveState).toBe('active')
    })

    it('sets unit status', async () => {
      await client.setUnitStatus('town-os-testserver.service', 'restart')
    })

    it('replays logs via SSE', async () => {
      const entries = []
      for await (const entry of client.logReplay(
        'town-os-testserver.service',
      )) {
        entries.push(entry)
        if (entries.length >= 1) break
      }
      expect(entries.length).toBeGreaterThanOrEqual(1)
      expect(entries[0].Message).toBeTruthy()
    })
  })

  // --- Repositories ---

  describe('repositories', () => {
    beforeAll(async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
    })

    it('starts with no repositories', async () => {
      const repos = await client.listRepositories()
      expect(repos).toEqual([])
    })

    it('lists no packages initially', async () => {
      const pkgs = await client.listPackages()
      expect(pkgs).toEqual([])
    })
  })

  // --- Audit log ---

  describe('audit log', () => {
    beforeAll(async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
    })

    it('lists audit entries from prior actions', async () => {
      const page = await client.listAuditLog({ limit: 100 })
      expect(Array.isArray(page.entries)).toBe(true)
      expect(page.entries.length).toBeGreaterThan(0)
      expect(typeof page.has_more).toBe('boolean')
    })

    it('filters by account', async () => {
      const page = await client.listAuditLog({
        limit: 100,
        account: 'admin',
      })
      for (const entry of page.entries) {
        expect(entry.account).toBe('admin')
      }
    })
  })

  // --- Error handling ---

  describe('error handling', () => {
    it('rejects authentication with wrong password', async () => {
      await expect(
        client.authenticate('admin', 'wrongpass'),
      ).rejects.toThrow()
    })

    it('rejects unauthenticated access to protected routes', async () => {
      const noAuth = new SystemControllerClient(baseURL)
      await expect(noAuth.listUnits()).rejects.toThrow()
    })
  })
})
