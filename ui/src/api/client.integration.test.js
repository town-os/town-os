import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { SystemControllerClient } from './client.js'

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
      const resp = await fetch(`${baseURL}/`)
      expect(resp.status).toBe(401)
    })

    it('returns username as json when authenticated', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      token = resp.token
      client.setToken(token)
      const fetchResp = await fetch(`${baseURL}/`, {
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
      const units = await client.listUnits()
      expect(units.length).toBeGreaterThan(0)
      const testserver = units.find(
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
  })

  // --- Repositories ---

  describe('repositories', () => {
    it('starts with no repositories', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const repos = await client.listRepositories()
      expect(repos).toEqual([])
    })

    it('lists no packages initially', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      const pkgs = await client.listPackages()
      expect(pkgs).toEqual([])
    })

    it('adds a repository without credentials', async () => {
      if (repoUser) {
        return // skip: can't test without credentials when env credentials are set
      }
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.addRepository(
        '',
        'https://gitea.com/town-os/test-packages-core.git',
      )
      const repos = await client.listRepositories()
      expect(repos.length).toBe(1)
      expect(repos[0].name).toBe('test-packages-core')
      expect(repos[0].username).toBeFalsy()
      await client.removeRepository('test-packages-core')
    })

    it('adds a repository with credentials', async () => {
      if (!repoUser) {
        return // skip: need valid credentials to test authenticated add
      }
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.addRepository(
        '',
        'https://gitea.com/town-os/test-packages-core.git',
        repoUser,
        repoPass,
      )
      const repos = await client.listRepositories()
      expect(repos.length).toBe(1)
      expect(repos[0].username).toBe(repoUser)
      await client.removeRepository('test-packages-core')
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
      const repos = await client.listRepositories()
      expect(repos).toEqual([])
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
  })

  // --- Package install creates systemd unit ---

  describe('package install creates systemd unit', () => {
    afterAll(async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      try {
        await client.uninstallPackage('nginx', '1.0')
      } catch (e) {
        console.warn('cleanup: uninstallPackage failed:', e.message)
      }
      await client.removeRepository('test-packages-core')
    })

    it('installs nginx@1.0 and creates a running systemd unit', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.addRepository(
        '',
        'https://gitea.com/town-os/test-packages-core.git',
        repoUser,
        repoPass,
      )
      await client.installPackage('nginx', '1.0', {
        hostname: 'testhost',
        port: '8081',
      })

      // give the unit time to start
      await new Promise((r) => setTimeout(r, 2000))

      const units = await client.listUnits()
      const unit = units.find((u) => u.Name === 'town-os-nginx.service')
      expect(unit).toBeDefined()
      expect(unit.ActiveState).toBe('active')
    })

    it('restarts the unit and it stays active', async () => {
      const resp = await client.authenticate('admin', 'adminpass')
      client.setToken(resp.token)
      await client.setUnitStatus('town-os-nginx.service', 'restart')

      // give time to restart
      await new Promise((r) => setTimeout(r, 2000))

      const units = await client.listUnits()
      const unit = units.find((u) => u.Name === 'town-os-nginx.service')
      expect(unit).toBeDefined()
      expect(unit.ActiveState).toBe('active')
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
      await client.uninstallPackage('nginx', '1.0')

      const units = await client.listUnits()
      const unit = units.find((u) => u.Name === 'town-os-nginx.service')
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
      ).rejects.toThrow(/POST \/account\/create:.*status 401/)
    })

    it('getAccount requires auth', async () => {
      await expect(
        noAuth.getAccount('admin'),
      ).rejects.toThrow(/POST \/account:.*status 401/)
    })

    it('updateAccount requires auth', async () => {
      await expect(
        noAuth.updateAccount('admin', { real_name: 'X' }),
      ).rejects.toThrow(/POST \/account\/update:.*status 401/)
    })

    it('listAccounts requires auth', async () => {
      await expect(
        noAuth.listAccounts(),
      ).rejects.toThrow(/GET \/account:.*status 401/)
    })

    it('disableAccount requires auth', async () => {
      await expect(
        noAuth.disableAccount('admin'),
      ).rejects.toThrow(/POST \/account\/disable:.*status 401/)
    })

    // Storage methods

    it('createFilesystem requires auth', async () => {
      await expect(
        noAuth.createFilesystem({ name: 'x' }),
      ).rejects.toThrow(/POST \/storage\/create:.*status 401/)
    })

    it('modifyFilesystem requires auth', async () => {
      await expect(
        noAuth.modifyFilesystem('x', { name: 'x' }),
      ).rejects.toThrow(/POST \/storage\/modify:.*status 401/)
    })

    it('removeFilesystem requires auth', async () => {
      await expect(
        noAuth.removeFilesystem('x'),
      ).rejects.toThrow(/POST \/storage\/remove:.*status 401/)
    })

    it('listFilesystems requires auth', async () => {
      await expect(
        noAuth.listFilesystems(''),
      ).rejects.toThrow(/POST \/storage:.*status 401/)
    })

    // Repository methods

    it('addRepository requires auth', async () => {
      await expect(
        noAuth.addRepository('', 'http://example.com'),
      ).rejects.toThrow(/POST \/repository\/add:.*status 401/)
    })

    it('removeRepository requires auth', async () => {
      await expect(
        noAuth.removeRepository('x'),
      ).rejects.toThrow(/POST \/repository\/remove:.*status 401/)
    })

    it('listRepositories requires auth', async () => {
      await expect(
        noAuth.listRepositories(),
      ).rejects.toThrow(/GET \/repository:.*status 401/)
    })

    // Package methods

    it('listPackages requires auth', async () => {
      await expect(
        noAuth.listPackages(),
      ).rejects.toThrow(/GET \/packages:.*status 401/)
    })

    it('listInstalled requires auth', async () => {
      await expect(
        noAuth.listInstalled(),
      ).rejects.toThrow(/GET \/packages\/installed:.*status 401/)
    })

    it('getResponses requires auth', async () => {
      await expect(
        noAuth.getResponses('x', '1.0'),
      ).rejects.toThrow(/POST \/packages\/responses:.*status 401/)
    })

    it('getPackageQuestions requires auth', async () => {
      await expect(
        noAuth.getPackageQuestions('x'),
      ).rejects.toThrow(/POST \/packages\/questions:.*status 401/)
    })

    it('installPackage requires auth', async () => {
      await expect(
        noAuth.installPackage('x', '1.0', {}),
      ).rejects.toThrow(/POST \/packages\/install:.*status 401/)
    })

    it('uninstallPackage requires auth', async () => {
      await expect(
        noAuth.uninstallPackage('x', '1.0'),
      ).rejects.toThrow(/POST \/packages\/uninstall:.*status 401/)
    })

    // Systemd methods

    it('listUnits requires auth', async () => {
      await expect(
        noAuth.listUnits(),
      ).rejects.toThrow(/GET \/systemd\/units:.*status 401/)
    })

    it('setUnitStatus requires auth', async () => {
      await expect(
        noAuth.setUnitStatus('x', 'restart'),
      ).rejects.toThrow(/POST \/systemd\/status:.*status 401/)
    })

    it('logReplay requires auth', async () => {
      const gen = noAuth.logReplay('x')
      await expect(gen.next()).rejects.toThrow(
        /GET \/systemd\/logs:.*status 401/,
      )
    })

    // Audit

    it('listAuditLog requires auth', async () => {
      await expect(
        noAuth.listAuditLog({}),
      ).rejects.toThrow(/POST \/audit\/log:.*status 401/)
    })

    // Session methods (explicit token)

    it('listSessions requires auth', async () => {
      await expect(
        noAuth.listSessions(''),
      ).rejects.toThrow(/GET \/account\/sessions:.*status 401/)
    })

    it('sessionUsername requires auth', async () => {
      await expect(
        noAuth.sessionUsername(''),
      ).rejects.toThrow(/GET \/:.*status 401/)
    })

  })
})
