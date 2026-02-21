import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { SystemControllerClient, ApiError } from './client.js'

const baseURL = process.env.INTEGRATION_URL
if (!baseURL) {
  throw new Error('INTEGRATION_URL environment variable is required')
}

const repoUser = process.env.TOWN_OS_REPO_USERNAME || ''
const repoPass = process.env.TOWN_OS_REPO_PASSWORD || ''

describe('SystemControllerClient integration', () => {
  /** @type {SystemControllerClient} */
  let client

  beforeAll(async () => {
    client = new SystemControllerClient(baseURL)
    await client.createAccount(
      'admin',
      'adminpass',
      'admin@test.com',
      '555-0001',
      'Test Admin',
      true,
    )
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
      expect(typeof resp.admins).toBe('number')
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
    it('authenticates and gets token', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      expect(resp.token).toBeTruthy()
      expect(resp.account.username).toBe('admin')
      client.setToken(resp.token)
    })

    it('gets account by username', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const acct = await client.getAccount('admin')
      expect(acct.username).toBe('admin')
      expect(acct.real_name).toBe('Test Admin')
    })

    it('updates account fields', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const acct = await client.updateAccount('admin', {
        real_name: 'Updated Admin',
      })
      expect(acct.real_name).toBe('Updated Admin')
    })

    it('lists accounts', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const accounts = await client.listAccounts()
      expect(accounts.length).toBeGreaterThanOrEqual(1)
      expect(accounts.some((a) => a.username === 'admin')).toBe(true)
    })

    it('creates a second account (requires auth)', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
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
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.disableAccount('user1')
      const acct = await client.getAccount('user1')
      expect(acct.disabled).toBe(true)
    })

    it('enables a disabled account', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.enableAccount('user1')
      const acct = await client.getAccount('user1')
      expect(acct.disabled).toBe(false)
    })

    it('admin cannot promote another user', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.updateAccount('user1', { admin: true }),
      ).rejects.toThrow()
    })

    it('admin cannot demote another user', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.updateAccount('user1', { admin: false }),
      ).rejects.toThrow()
    })

    it('non-admin cannot promote a user', async () => {
      const resp = await client.authenticate('user1', 'userpass')
      client.setToken(resp.token)
      await expect(
        client.updateAccount('user1', { admin: true }),
      ).rejects.toThrow()
    })

    it('non-admin cannot demote an admin', async () => {
      const resp = await client.authenticate('user1', 'userpass')
      client.setToken(resp.token)
      await expect(
        client.updateAccount('admin', { admin: false }),
      ).rejects.toThrow()
    })

    it('admin cannot change own admin status', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.updateAccount('admin', { admin: false }),
      ).rejects.toThrow()
    })
  })

  // --- Storage lifecycle ---

  describe('storage lifecycle', () => {
    // Note: quota tests are omitted because the test btrfs filesystem
    // does not have quotas enabled.

    it('creates a filesystem', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.createFilesystem({ name: 'testfs', quota: 0 })
      const list = await client.listFilesystems('')
      expect(list.some((f) => f.name === 'testfs')).toBe(true)
    })

    it('lists filesystems', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const list = await client.listFilesystems('')
      expect(list.length).toBeGreaterThanOrEqual(1)
      expect(list.some((f) => f.name === 'testfs')).toBe(true)
    })

    it('modifies filesystem name (rename)', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.modifyFilesystem('testfs', { name: 'renamedfs', quota: 0 })
      const list = await client.listFilesystems('')
      expect(list.some((f) => f.name === 'renamedfs')).toBe(true)
      expect(list.some((f) => f.name === 'testfs')).toBe(false)
    })

    it('rejects invalid filesystem name', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.createFilesystem({ name: '/badname', quota: 0 }),
      ).rejects.toThrow()
    })

    it('rejects modify of root filesystem', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.modifyFilesystem('', { name: 'stolen', quota: 0 }),
      ).rejects.toThrow()
    })

    it('removes a filesystem', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.removeFilesystem('renamedfs')
      const list = await client.listFilesystems('')
      expect(list.some((f) => f.name === 'renamedfs')).toBe(false)
    })
  })

  // --- Session management ---

  describe('sessions', () => {
    /** @type {string} */
    let token

    it('lists sessions for token', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      token = resp.token
      client.setToken(token)
      const sessions = await client.listSessions(token)
      expect(Array.isArray(sessions)).toBe(true)
      expect(sessions.length).toBeGreaterThanOrEqual(1)
      expect(sessions[0].username).toBe('admin')
    })

    it('rejects session username without auth', async () => {
      const resp = await fetch(`${baseURL}/account/me`)
      expect(resp.status).toBe(401)
    })

    it('returns username as json when authenticated', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      token = resp.token
      client.setToken(token)
      const fetchResp = await fetch(`${baseURL}/account/me`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      expect(fetchResp.status).toBe(200)
      const body = await fetchResp.json()
      expect(body).toEqual({ username: 'admin' })
    })

    it('creates and revokes a session', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      token = resp.token
      client.setToken(token)
      await client.authenticate('admin', 'adminpass')

      const before = await client.listSessions(token)
      await client.revokeSession(before[before.length - 1].id)
      const after = await client.listSessions(token)
      expect(after.length).toBe(before.length - 1)
    })
  })

  // --- Systemd ---

  describe('systemd', () => {
    it('lists units from real systemd', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const result = await client.listUnits(
        undefined,
        undefined,
        undefined,
        undefined,
        'town-os-testserver',
      )
      expect(result.entries.length).toBeGreaterThan(0)
      const testserver = result.entries.find(
        (u) => u.Name === 'town-os-testserver.service',
      )
      expect(testserver).toBeDefined()
      expect(testserver.ActiveState).toBe('active')
    })

    it('sets unit status', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-test.service', 'restart')
    })

    it('starts a unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-test.service', 'start')
    })

    it('stops a unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-test.service', 'stop')
    })

    it('enables a unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-test.service', 'enable')
    })

    it('disables a unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-test.service', 'disable')
    })

    it('rejects invalid action', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.setUnitStatus('town-os-test.service', 'invalid'),
      ).rejects.toThrow()
    })

    it('replays logs via SSE', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
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

    it('tails the last N log entries', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const result = await client.logTail('town-os-testserver.service', 5)
      expect(result.entries.length).toBeGreaterThan(0)
      expect(result.entries.length).toBeLessThanOrEqual(5)
      expect(result.entries[0].Message).toBeTruthy()
      expect(result.cursor).toBeTruthy()
    })

    it('paginates backwards with cursor', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const first = await client.logTail('town-os-testserver.service', 3)
      expect(first.cursor).toBeTruthy()

      const older = await client.logTail(
        'town-os-testserver.service',
        3,
        first.cursor,
      )
      expect(Array.isArray(older.entries)).toBe(true)
      // Older page entries should have earlier timestamps
      if (older.entries.length > 0 && first.entries.length > 0) {
        const oldestNew = new Date(first.entries[0].RealtimeTimestamp)
        const newestOld = new Date(
          older.entries[older.entries.length - 1].RealtimeTimestamp,
        )
        expect(newestOld.getTime()).toBeLessThanOrEqual(oldestNew.getTime())
      }
    })
  })

  // --- Repositories ---

  describe('repositories', () => {
    it('starts with default repositories', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const result = await client.listRepositories()
      expect(result.entries.length).toBe(2)
      expect(result.entries.some((r) => r.name === 'core')).toBe(true)
      expect(result.entries.some((r) => r.name === 'extras')).toBe(true)
    })

    it('lists packages from default repositories', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const result = await client.listPackages()
      expect(result.entries.length).toBeGreaterThan(0)
    })

    it('refreshes repositories', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.refreshRepositories()
      const result = await client.listRepositories()
      expect(result.entries.length).toBe(2)
    })

    it('adds a repository without credentials', async () => {
      if (repoUser) {
        return // skip: can't test without credentials when env credentials are set
      }
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.addRepository(
        'extra-core',
        'https://gitea.com/town-os/test-packages-core.git',
      )
      const result = await client.listRepositories()
      expect(result.entries.length).toBe(3)
      expect(result.entries.some((r) => r.name === 'extra-core')).toBe(true)
      await client.removeRepository('extra-core')
    })

    it('adds a repository with credentials', async () => {
      if (!repoUser) {
        return // skip: need valid credentials to test authenticated add
      }
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.addRepository(
        'extra-core',
        'https://gitea.com/town-os/test-packages-core.git',
        repoUser,
        repoPass,
      )
      const result = await client.listRepositories()
      expect(result.entries.length).toBe(3)
      expect(result.entries.find((r) => r.name === 'extra-core').username).toBe(
        repoUser,
      )
      await client.removeRepository('extra-core')
    })

    it('rejects partial credentials (username only)', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.addRepository(
          '',
          'https://gitea.com/town-os/test-packages-core.git',
          'user',
          '',
        ),
      ).rejects.toThrow()
    })

    it('rejects partial credentials (password only)', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.addRepository(
          '',
          'https://gitea.com/town-os/test-packages-core.git',
          '',
          'pass',
        ),
      ).rejects.toThrow()
    })

    it('fails to add with bad clone URL', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.addRepository(
          '',
          'https://gitea.com/town-os/does-not-exist.git',
        ),
      ).rejects.toThrow()
      const result = await client.listRepositories()
      expect(result.entries.length).toBe(2)
    })

    it('fails to remove nonexistent repository', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.removeRepository('nonexistent'),
      ).rejects.toThrow()
    })
  })

  // --- Audit log ---

  describe('audit log', () => {
    it('lists audit entries from prior actions', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const page = await client.listAuditLog({ limit: 100 })
      expect(Array.isArray(page.entries)).toBe(true)
      expect(page.entries.length).toBeGreaterThan(0)
      expect(typeof page.has_more).toBe('boolean')
    })

    it('filters by account', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const page = await client.listAuditLog({
        limit: 100,
        account: 'admin',
      })
      for (const entry of page.entries) {
        expect(entry.account).toBe('admin')
      }
    })

    it('includes detail field with request parameters', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const page = await client.listAuditLog({ limit: 100 })
      const createEntry = page.entries.find(
        (e) => e.action === 'create account' && e.detail,
      )
      expect(createEntry).toBeDefined()
      expect(createEntry.detail).toContain('admin')
      // Password must be redacted from detail.
      expect(createEntry.detail).not.toContain('adminpass')
    })

    it('detail field is valid JSON containing POST body fields', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const page = await client.listAuditLog({ limit: 100 })
      const createEntry = page.entries.find(
        (e) => e.action === 'create account' && e.detail && e.detail.includes('user1'),
      )
      expect(createEntry).toBeDefined()
      const parsed = JSON.parse(createEntry.detail)
      expect(parsed.username).toBe('user1')
      expect(parsed.email).toBe('user1@test.com')
      expect(parsed.real_name).toBe('Regular User')
      expect(parsed.admin).toBe(false)
      // Password must never appear in detail
      expect(parsed.password).toBeUndefined()
    })

    it('detail never contains password for any entry', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const page = await client.listAuditLog({ limit: 200 })
      for (const entry of page.entries) {
        if (entry.detail) {
          expect(entry.detail).not.toContain('adminpass')
          expect(entry.detail).not.toContain('"password"')
        }
      }
    })

    it('captures detail for authenticate action', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const page = await client.listAuditLog({ limit: 100 })
      const authEntry = page.entries.find(
        (e) => e.action === 'authenticate' && e.detail,
      )
      expect(authEntry).toBeDefined()
      const parsed = JSON.parse(authEntry.detail)
      expect(parsed.username).toBe('admin')
      expect(parsed.password).toBeUndefined()
    })
  })

  // --- Settings ---

  describe('settings', () => {
    it('sets and gets a setting', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setSetting('default_quota', '53687091200')
      const value = await client.getSetting('default_quota')
      expect(value).toBe('53687091200')
    })

    it('lists all settings', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const settings = await client.getSettings()
      expect(settings.default_quota).toBe('53687091200')
    })

    it('overwrites a setting', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setSetting('default_quota', '0')
      const value = await client.getSetting('default_quota')
      expect(value).toBe('0')
    })

    it('returns 404 for nonexistent setting', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await expect(
        client.getSetting('nonexistent'),
      ).rejects.toThrow()
    })
  })

  // --- Package install creates systemd unit ---

  describe('package install creates systemd unit', () => {
    afterAll(async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      try {
        await client.uninstallPackage('nginx', '1.0', true)
      } catch (e) {
        console.warn('cleanup: uninstallPackage failed:', e.message)
      }
    })

    it('installs nginx@1.0 and creates a running systemd unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.installPackage('nginx', '1.0', {
        hostname: 'testhost',
        port: '8081',
      })

      // give the unit time to start
      await new Promise((r) => setTimeout(r, 2000))

      const result = await client.listUnits(
        undefined,
        undefined,
        undefined,
        undefined,
        'town-os-nginx',
      )
      const unit = result.entries.find(
        (u) => u.Name === 'town-os-nginx.service',
      )
      expect(unit).toBeDefined()
      expect(unit.ActiveState).toBe('active')
    })

    it('returns installed info with questions and responses', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const info = await client.getInstalledInfo('nginx', '1.0')
      expect(info.questions).toBeDefined()
      expect(info.questions.hostname.query).toBe('What hostname should nginx serve?')
      expect(info.questions.port.query).toBe('What external port should nginx listen on?')
      expect(info.responses).toBeDefined()
      expect(info.responses.hostname).toBe('testhost')
      expect(info.responses.port).toBe('8081')
      // notes are only present if the remote package defines them
      if (info.notes) {
        expect(info.notes.URL).toBe('http://testhost:8081')
      }
    })

    it('restarts the unit and it stays active', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-nginx.service', 'restart')

      // give time to restart
      await new Promise((r) => setTimeout(r, 2000))

      const result = await client.listUnits(
        undefined,
        undefined,
        undefined,
        undefined,
        'town-os-nginx',
      )
      const unit = result.entries.find(
        (u) => u.Name === 'town-os-nginx.service',
      )
      expect(unit).toBeDefined()
      expect(unit.ActiveState).toBe('active')
    })

    it('stops the unit and it becomes inactive', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-nginx.service', 'stop')

      await new Promise((r) => setTimeout(r, 2000))

      const result = await client.listUnits(
        undefined,
        undefined,
        undefined,
        undefined,
        'town-os-nginx',
      )
      const unit = result.entries.find(
        (u) => u.Name === 'town-os-nginx.service',
      )
      expect(unit).toBeDefined()
      expect(unit.ActiveState).not.toBe('active')
    })

    it('starts the unit back and it becomes active again', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-nginx.service', 'start')

      await new Promise((r) => setTimeout(r, 2000))

      const result = await client.listUnits(
        undefined,
        undefined,
        undefined,
        undefined,
        'town-os-nginx',
      )
      const unit = result.entries.find(
        (u) => u.Name === 'town-os-nginx.service',
      )
      expect(unit).toBeDefined()
      expect(unit.ActiveState).toBe('active')
    })

    it('disables the unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-nginx.service', 'disable')
    })

    it('enables the unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-nginx.service', 'enable')
    })

    it('replays logs containing the running message', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const entries = []
      for await (const entry of client.logReplay('town-os-nginx.service')) {
        entries.push(entry)
        if (entries.length >= 3) break
      }
      expect(entries.length).toBeGreaterThanOrEqual(1)
      const hasRunning = entries.some((e) =>
        e.Message.includes('nginx@1.0 running'),
      )
      expect(hasRunning).toBe(true)
    })

    it('uninstalls nginx@1.0 and the unit is gone', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.uninstallPackage('nginx', '1.0', false)

      const result = await client.listUnits(
        undefined,
        undefined,
        undefined,
        undefined,
        'town-os-nginx',
      )
      const unit = result.entries.find(
        (u) => u.Name === 'town-os-nginx.service',
      )
      expect(
        unit === undefined || unit.ActiveState !== 'active',
      ).toBe(true)
    })
  })

  // --- Error handling ---

  describe('error handling', () => {
    /** @type {SystemControllerClient} */
    let noAuth

    beforeAll(() => {
      noAuth = new SystemControllerClient(baseURL)
    })

    it('rejects authentication with wrong password', async () => {
      await expect(
        client.authenticate('admin', 'wrongpass'),
      ).rejects.toThrow()
    })

    // Account methods

    it('createAccount requires auth', async () => {
      await expect(
        noAuth.createAccount('x', 'x', 'x@x.com', '0', 'X', false),
      ).rejects.toThrow(/POST \/account\/create:.*missing authorization token/)
    })

    it('getAccount requires auth', async () => {
      await expect(
        noAuth.getAccount('admin'),
      ).rejects.toThrow(/POST \/account:.*missing authorization token/)
    })

    it('updateAccount requires auth', async () => {
      await expect(
        noAuth.updateAccount('admin', { real_name: 'X' }),
      ).rejects.toThrow(/POST \/account\/update:.*missing authorization token/)
    })

    it('listAccounts requires auth', async () => {
      await expect(
        noAuth.listAccounts(),
      ).rejects.toThrow(/GET \/account:.*missing authorization token/)
    })

    it('disableAccount requires auth', async () => {
      await expect(
        noAuth.disableAccount('admin'),
      ).rejects.toThrow(/POST \/account\/disable:.*missing authorization token/)
    })

    // Storage methods

    it('createFilesystem requires auth', async () => {
      await expect(
        noAuth.createFilesystem({ name: 'x' }),
      ).rejects.toThrow(/POST \/storage\/create:.*missing authorization token/)
    })

    it('modifyFilesystem requires auth', async () => {
      await expect(
        noAuth.modifyFilesystem('x', { name: 'x' }),
      ).rejects.toThrow(/POST \/storage\/modify:.*missing authorization token/)
    })

    it('removeFilesystem requires auth', async () => {
      await expect(
        noAuth.removeFilesystem('x'),
      ).rejects.toThrow(/POST \/storage\/remove:.*missing authorization token/)
    })

    it('listFilesystems requires auth', async () => {
      await expect(
        noAuth.listFilesystems(''),
      ).rejects.toThrow(/POST \/storage:.*missing authorization token/)
    })

    // Repository methods

    it('addRepository requires auth', async () => {
      await expect(
        noAuth.addRepository('', 'http://example.com'),
      ).rejects.toThrow(/POST \/repository\/add:.*missing authorization token/)
    })

    it('removeRepository requires auth', async () => {
      await expect(
        noAuth.removeRepository('x'),
      ).rejects.toThrow(/POST \/repository\/remove:.*missing authorization token/)
    })

    it('refreshRepositories requires auth', async () => {
      await expect(
        noAuth.refreshRepositories(),
      ).rejects.toThrow(/POST \/repository\/refresh:.*missing authorization token/)
    })

    it('listRepositories requires auth', async () => {
      await expect(
        noAuth.listRepositories(),
      ).rejects.toThrow(/GET \/repository:.*missing authorization token/)
    })

    // Package methods

    it('listPackages requires auth', async () => {
      await expect(
        noAuth.listPackages(),
      ).rejects.toThrow(/GET \/packages:.*missing authorization token/)
    })

    it('listInstalled requires auth', async () => {
      await expect(
        noAuth.listInstalled(),
      ).rejects.toThrow(/GET \/packages\/installed:.*missing authorization token/)
    })

    it('getResponses requires auth', async () => {
      await expect(
        noAuth.getResponses('x', '1.0'),
      ).rejects.toThrow(/POST \/packages\/responses:.*missing authorization token/)
    })

    it('getInstalledInfo requires auth', async () => {
      await expect(
        noAuth.getInstalledInfo('x', '1.0'),
      ).rejects.toThrow(/POST \/packages\/installed\/info:.*missing authorization token/)
    })

    it('getPackageQuestions requires auth', async () => {
      await expect(
        noAuth.getPackageQuestions('x'),
      ).rejects.toThrow(/POST \/packages\/questions:.*missing authorization token/)
    })

    it('installPackage requires auth', async () => {
      await expect(
        noAuth.installPackage('x', '1.0', {}),
      ).rejects.toThrow(/POST \/packages\/install:.*missing authorization token/)
    })

    it('uninstallPackage requires auth', async () => {
      await expect(
        noAuth.uninstallPackage('x', '1.0'),
      ).rejects.toThrow(/POST \/packages\/uninstall:.*missing authorization token/)
    })

    it('purgeVolumes requires auth', async () => {
      await expect(
        noAuth.purgeVolumes('x'),
      ).rejects.toThrow(/POST \/packages\/purge-volumes:.*missing authorization token/)
    })

    // Systemd methods

    it('listUnits requires auth', async () => {
      await expect(
        noAuth.listUnits(),
      ).rejects.toThrow(/GET \/systemd\/units:.*missing authorization token/)
    })

    it('setUnitStatus requires auth', async () => {
      await expect(
        noAuth.setUnitStatus('x', 'restart'),
      ).rejects.toThrow(/POST \/systemd\/status:.*missing authorization token/)
    })

    it('logReplay requires auth', async () => {
      const gen = noAuth.logReplay('x')
      await expect(gen.next()).rejects.toThrow(
        /GET \/systemd\/logs:.*missing authorization token/,
      )
    })

    it('logTail requires auth', async () => {
      await expect(
        noAuth.logTail('x'),
      ).rejects.toThrow(/GET \/systemd\/logs\/tail.*missing authorization token/)
    })

    // Audit

    it('listAuditLog requires auth', async () => {
      await expect(
        noAuth.listAuditLog({}),
      ).rejects.toThrow(/POST \/audit\/log:.*missing authorization token/)
    })

    // Settings methods

    it('getSettings requires auth', async () => {
      await expect(
        noAuth.getSettings(),
      ).rejects.toThrow(/GET \/settings:.*missing authorization token/)
    })

    it('getSetting requires auth', async () => {
      await expect(
        noAuth.getSetting('default_quota'),
      ).rejects.toThrow(/POST \/settings\/get:.*missing authorization token/)
    })

    it('setSetting requires auth', async () => {
      await expect(
        noAuth.setSetting('default_quota', '0'),
      ).rejects.toThrow(/POST \/settings\/set:.*missing authorization token/)
    })

    // Session methods (explicit token)

    it('listSessions requires auth', async () => {
      await expect(
        noAuth.listSessions(''),
      ).rejects.toThrow(/GET \/account\/sessions:.*missing authorization token/)
    })

    it('sessionUsername requires auth', async () => {
      await expect(
        noAuth.sessionUsername(''),
      ).rejects.toThrow(/GET \/account\/me:.*missing authorization token/)
    })

    // Problem detail structure

    it('error includes problem detail fields', async () => {
      try {
        await noAuth.listAccounts()
        expect.unreachable('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.status).toBe(401)
        expect(err.detail).toBe('missing authorization token')
        expect(err.problem).not.toBeNull()
        expect(err.problem.status).toBe(401)
        expect(err.problem.detail).toBe('missing authorization token')
        expect(err.problem.type).toBe('about:blank#401')
      }
    })

    it('error message includes method, path, and detail', async () => {
      try {
        await noAuth.createFilesystem({ name: 'x' })
        expect.unreachable('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.message).toContain('POST')
        expect(err.message).toContain('/storage/create')
        expect(err.message).toContain('missing authorization token')
        expect(err.message).not.toContain('status 401')
      }
    })

    it('wrong password error includes detail from server', async () => {
      try {
        await client.authenticate('admin', 'wrongpass')
        expect.unreachable('should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError)
        expect(err.status).toBe(401)
        expect(err.detail).toBeTruthy()
        expect(err.message).toContain('POST')
        expect(err.message).toContain('/account/authenticate')
        expect(err.message).not.toContain('Internal Server Error')
      }
    })

  })
})
