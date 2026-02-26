/** @import { Filesystem, UnitStatus, JournalEntry, StatusAction, Account, UpdateFields, Session, AuditListOptions, AuditPage, Question, Responses, InstalledInfo, RepositoryInfo, PingResponse, AuthenticateResponse, PackageUpgrade } from './types.js' */

export class ApiError extends Error {
  /**
   * @param {string} method
   * @param {string} path
   * @param {number} status
   * @param {string} body
   */
  constructor(method, path, status, body) {
    const detail = ApiError.parseDetail(body)
    const displayDetail = detail || `status ${status}`
    super(`${method} ${path}: ${displayDetail}`)
    this.name = 'ApiError'
    this.status = status
    this.path = path
    this.body = body
    this.detail = detail
    this.problem = ApiError.parseProblem(body)
  }

  /**
   * Extract human-readable detail from a response body.
   * Tries RFC 9457 problem+json first, then legacy echo format.
   * @param {string} body
   * @returns {string}
   */
  static parseDetail(body) {
    if (!body) return ''
    try {
      const parsed = JSON.parse(body)
      if (parsed.detail) return parsed.detail
      if (parsed.message) return parsed.message
    } catch {
      // not JSON
    }
    return body
  }

  /**
   * Parse as RFC 9457 problem detail object if valid.
   * @param {string} body
   * @returns {object|null}
   */
  static parseProblem(body) {
    if (!body) return null
    try {
      const parsed = JSON.parse(body)
      if (parsed.type && typeof parsed.status === 'number') return parsed
    } catch {
      // not JSON
    }
    return null
  }
}

export class SystemControllerClient {
  /** @param {string} baseURL */
  constructor(baseURL) {
    this.baseURL = baseURL.replace(/\/+$/, '')
    this.token = ''
  }

  /** @param {string} token */
  setToken(token) {
    this.token = token
  }

  /**
   * @param {string} path
   * @param {any} [body]
   * @returns {Promise<Response>}
   */
  async post(path, body) {
    /** @type {HeadersInit} */
    const headers = { 'Content-Type': 'application/json' }
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }
    const resp = await fetch(`${this.baseURL}${path}`, {
      method: 'POST',
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
    if (resp.status !== 200) {
      const text = await resp.text().catch(() => '')
      throw new ApiError('POST', path, resp.status, text)
    }
    return resp
  }

  /**
   * @param {string} path
   * @param {HeadersInit} [extraHeaders]
   * @returns {Promise<Response>}
   */
  async get(path, extraHeaders) {
    /** @type {HeadersInit} */
    const headers = { ...extraHeaders }
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }
    const resp = await fetch(`${this.baseURL}${path}`, { headers })
    if (resp.status !== 200) {
      const text = await resp.text().catch(() => '')
      throw new ApiError('GET', path, resp.status, text)
    }
    return resp
  }

  /**
   * @template T
   * @param {string} path
   * @param {any} [body]
   * @returns {Promise<T>}
   */
  async postJSON(path, body) {
    const resp = await this.post(path, body)
    return resp.json()
  }

  /**
   * @template T
   * @param {string} path
   * @param {HeadersInit} [extraHeaders]
   * @returns {Promise<T>}
   */
  async getJSON(path, extraHeaders) {
    const resp = await this.get(path, extraHeaders)
    return resp.json()
  }

  // --- Public (no token required) ---

  /** @returns {Promise<PingResponse>} */
  async ping() {
    return this.getJSON('/status/ping')
  }

  /**
   * @param {string} username
   * @param {string} password
   * @returns {Promise<AuthenticateResponse>}
   */
  async authenticate(username, password) {
    return this.postJSON('/account/authenticate', { username, password })
  }

  // --- Self-authenticated (explicit token in handler) ---

  /**
   * @param {string} token
   * @returns {Promise<Session[]>}
   */
  async listSessions(token) {
    return this.getJSON('/account/sessions', {
      Authorization: `Bearer ${token}`,
    })
  }

  /**
   * @param {string} token
   * @returns {Promise<string>}
   */
  async sessionUsername(token) {
    /** @type {{ username: string }} */
    const result = await this.getJSON('/account/me', {
      Authorization: `Bearer ${token}`,
    })
    return result.username
  }

  /**
   * @param {string} sessionID
   * @returns {Promise<void>}
   */
  async revokeSession(sessionID) {
    await this.post('/account/session/revoke', { session_id: sessionID })
  }

  // --- Authenticated (uses this.token) ---

  /**
   * @param {Filesystem} fs
   * @returns {Promise<void>}
   */
  async createFilesystem(fs) {
    await this.post('/storage/create', fs)
  }

  /**
   * @param {string} name
   * @param {Filesystem} fs
   * @returns {Promise<void>}
   */
  async modifyFilesystem(name, fs) {
    await this.post('/storage/modify', { name, filesystem: fs })
  }

  /**
   * @param {string} name
   * @returns {Promise<void>}
   */
  async removeFilesystem(name) {
    await this.post('/storage/remove', { name })
  }

  /**
   * @param {string} prefix
   * @param {string} [sortBy]
   * @param {string} [sortOrder]
   * @param {string} [state]
   * @param {number} [limit]
   * @param {number} [offset]
   * @param {string} [search]
   * @returns {Promise<{entries: Filesystem[], has_more: boolean, total_pages: number, total_count: number}>}
   */
  async listFilesystems(prefix, sortBy, sortOrder, state, limit, offset, search) {
    const body = { name: prefix }
    if (sortBy) body.sort_by = sortBy
    if (sortOrder) body.sort_order = sortOrder
    if (state) body.state = state
    if (limit) body.limit = limit
    if (offset) body.offset = offset
    if (search) body.search = search
    return this.postJSON('/storage', body)
  }

  /**
   * @param {string} name
   * @param {string} url
   * @param {string} [username]
   * @param {string} [password]
   * @returns {Promise<void>}
   */
  async addRepository(name, url, username = '', password = '') {
    await this.post('/repository/add', { name, url, username, password })
  }

  /**
   * @param {string} name
   * @returns {Promise<void>}
   */
  async removeRepository(name) {
    await this.post('/repository/remove', { name })
  }

  /**
   * @param {string} name
   * @param {number} position
   * @returns {Promise<void>}
   */
  async moveRepository(name, position) {
    await this.post('/repository/move', { name, position })
  }

  /** @returns {Promise<Record<string, string>|null>} */
  async refreshRepositories() {
    const resp = await this.post('/repository/refresh', {})
    const text = await resp.text()
    if (!text) return null
    try {
      return JSON.parse(text)
    } catch {
      return null
    }
  }

  /**
   * @param {string} [sortBy]
   * @param {string} [sortOrder]
   * @param {number} [limit]
   * @param {number} [offset]
   * @param {string} [search]
   * @returns {Promise<{entries: RepositoryInfo[], has_more: boolean, total_pages: number}>}
   */
  async listRepositories(sortBy, sortOrder, limit, offset, search) {
    const params = new URLSearchParams()
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
    if (limit) params.set('limit', String(limit))
    if (offset) params.set('offset', String(offset))
    if (search) params.set('search', search)
    const qs = params.toString()
    return this.getJSON(`/repository${qs ? `?${qs}` : ''}`)
  }

  /**
   * @param {string} [sortBy]
   * @param {string} [sortOrder]
   * @param {number} [limit]
   * @param {number} [offset]
   * @param {string} [search]
   * @returns {Promise<{entries: string[], has_more: boolean, total_pages: number}>}
   */
  async listPackages(sortBy, sortOrder, limit, offset, search) {
    const params = new URLSearchParams()
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
    if (limit) params.set('limit', String(limit))
    if (offset) params.set('offset', String(offset))
    if (search) params.set('search', search)
    const qs = params.toString()
    return this.getJSON(`/packages${qs ? `?${qs}` : ''}`)
  }

  /**
   * @param {string} [search]
   * @returns {Promise<Array<{repo: string, packages: Array<{repo: string, name: string, version: string}>}>>}
   */
  async listPackagesByRepo(search) {
    const params = new URLSearchParams()
    if (search) params.set('search', search)
    const qs = params.toString()
    return this.getJSON(`/packages/by-repo${qs ? `?${qs}` : ''}`)
  }

  /**
   * @param {string} [sortBy]
   * @param {string} [sortOrder]
   * @param {number} [limit]
   * @param {number} [offset]
   * @param {string} [search]
   * @returns {Promise<{entries: string[], has_more: boolean, total_pages: number}>}
   */
  async listInstalled(sortBy, sortOrder, limit, offset, search) {
    const params = new URLSearchParams()
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
    if (limit) params.set('limit', String(limit))
    if (offset) params.set('offset', String(offset))
    if (search) params.set('search', search)
    const qs = params.toString()
    return this.getJSON(`/packages/installed${qs ? `?${qs}` : ''}`)
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @param {string} version
   * @returns {Promise<Responses>}
   */
  async getResponses(repo, name, version) {
    return this.postJSON('/packages/responses', { repo, name, version })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @param {string} version
   * @returns {Promise<InstalledInfo>}
   */
  async getInstalledInfo(repo, name, version) {
    return this.postJSON('/packages/installed/info', { repo, name, version })
  }

  /**
   * @param {string} [sortBy]
   * @param {string} [sortOrder]
   * @param {number} [limit]
   * @param {number} [offset]
   * @param {string} [search]
   * @returns {Promise<{entries: UnitStatus[], has_more: boolean, total_pages: number}>}
   */
  async listUnits(sortBy, sortOrder, limit, offset, search) {
    const params = new URLSearchParams()
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
    if (limit) params.set('limit', String(limit))
    if (offset) params.set('offset', String(offset))
    if (search) params.set('search', search)
    const qs = params.toString()
    return this.getJSON(`/systemd/units${qs ? `?${qs}` : ''}`)
  }

  /**
   * @param {string} unit
   * @returns {AsyncGenerator<JournalEntry>}
   */
  async *logReplay(unit) {
    /** @type {HeadersInit} */
    const headers = {}
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }
    const resp = await fetch(
      `${this.baseURL}/systemd/logs?unit=${encodeURIComponent(unit)}`,
      { headers },
    )
    if (resp.status !== 200) {
      const text = await resp.text().catch(() => '')
      throw new ApiError('GET', '/systemd/logs', resp.status, text)
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    for (;;) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          yield JSON.parse(line.slice(6))
        }
      }
    }

    if (buffer.startsWith('data: ')) {
      yield JSON.parse(buffer.slice(6))
    }
  }

  /**
   * @param {string} unit
   * @param {number} [lines=100]
   * @param {string} [beforeCursor]
   * @param {string} [afterCursor]
   * @param {string} [grep]
   * @param {number} [since] - Unix timestamp in seconds
   * @param {number} [until] - Unix timestamp in seconds
   * @param {number} [priority] - Max priority level (0=no filter, 3=error and above)
   * @returns {Promise<{entries: JournalEntry[], cursor: string, end_cursor: string}>}
   */
  async logTail(unit, lines = 100, beforeCursor, afterCursor, grep, since, until, priority) {
    const params = new URLSearchParams({ unit, lines: String(lines) })
    if (beforeCursor) params.set('before', beforeCursor)
    if (afterCursor) params.set('after', afterCursor)
    if (grep) params.set('grep', grep)
    if (since) params.set('since', String(since))
    if (until) params.set('until', String(until))
    if (priority) params.set('priority', String(priority))
    return this.getJSON(`/systemd/logs/tail?${params.toString()}`)
  }

  /**
   * @param {string} username
   * @param {string} password
   * @param {string} email
   * @param {string} phone
   * @param {string} realName
   * @param {boolean} admin
   * @returns {Promise<Account>}
   */
  async createAccount(username, password, email, phone, realName, admin) {
    return this.postJSON('/account/create', {
      username,
      password,
      email,
      phone,
      real_name: realName,
      admin,
    })
  }

  /**
   * @param {string} username
   * @returns {Promise<Account>}
   */
  async getAccount(username) {
    return this.postJSON('/account', { username })
  }

  /**
   * @param {string} username
   * @param {UpdateFields} fields
   * @returns {Promise<Account>}
   */
  async updateAccount(username, fields) {
    return this.postJSON('/account/update', { username, fields })
  }

  /**
   * @param {string} [sortBy]
   * @param {string} [sortOrder]
   * @param {number} [limit]
   * @param {number} [offset]
   * @param {string} [search]
   * @returns {Promise<{entries: Account[], has_more: boolean, total_pages: number, total_count: number}>}
   */
  async listAccounts(sortBy, sortOrder, limit, offset, search) {
    const params = new URLSearchParams()
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
    if (limit) params.set('limit', String(limit))
    if (offset) params.set('offset', String(offset))
    if (search) params.set('search', search)
    const qs = params.toString()
    return this.getJSON(`/account${qs ? `?${qs}` : ''}`)
  }

  // --- Admin (uses this.token, requires admin) ---

  /**
   * @param {string} name
   * @returns {Promise<Record<string, Question>>}
   */
  async getPackageQuestions(name) {
    return this.postJSON('/packages/questions', { name })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @param {string} version
   * @returns {Promise<Record<string, Question>>}
   */
  async getPackageQuestionsByIdentity(repo, name, version) {
    return this.postJSON('/packages/questions/identity', { repo, name, version })
  }

  /**
   * @param {string} name
   * @returns {Promise<string[]>}
   */
  async listPackageVersions(name) {
    return this.postJSON('/packages/versions', { name })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @returns {Promise<{has_uninstalled_volumes: boolean, uninstalled_versions?: string[], installed_versions?: string[]}>}
   */
  async listUninstalledVolumes(repo, name) {
    return this.postJSON('/packages/uninstalled-volumes', { repo, name })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @returns {Promise<void>}
   */
  async purgeUninstalledVolumes(repo, name) {
    await this.post('/packages/purge-uninstalled-volumes', { repo, name })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @param {string} version
   * @returns {Promise<object>}
   */
  async installPreview(repo, name, version) {
    return this.postJSON('/packages/install-preview', { repo, name, version })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @param {string} version
   * @param {Responses} responses
   * @param {boolean} [reuseVolumes=false]
   * @param {string} [importFromVersion]
   * @returns {Promise<void>}
   */
  async installPackage(repo, name, version, responses, reuseVolumes = false, importFromVersion) {
    const body = { repo, name, version, responses }
    if (reuseVolumes) body.reuse_volumes = true
    if (importFromVersion) body.import_from_version = importFromVersion
    await this.post('/packages/install', body)
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @param {string} version
   * @param {boolean} [purgeVolumes=false]
   * @returns {Promise<void>}
   */
  async uninstallPackage(repo, name, version, purgeVolumes = false) {
    await this.post('/packages/uninstall', { repo, name, version, purge_volumes: purgeVolumes })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @returns {Promise<void>}
   */
  async purgeVolumes(repo, name) {
    await this.post('/packages/purge-volumes', { repo, name })
  }

  /**
   * @param {string} name
   * @param {StatusAction} action
   * @returns {Promise<void>}
   */
  async setUnitStatus(name, action) {
    await this.post('/systemd/status', { name, action })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @returns {Promise<void>}
   */
  async disablePackage(repo, name) {
    await this.post('/packages/disable', { repo, name })
  }

  /**
   * @param {string} repo
   * @param {string} name
   * @returns {Promise<void>}
   */
  async enablePackage(repo, name) {
    await this.post('/packages/enable', { repo, name })
  }

  /**
   * @param {string} username
   * @returns {Promise<void>}
   */
  async disableAccount(username) {
    await this.post('/account/disable', { username })
  }

  /**
   * @param {string} username
   * @returns {Promise<void>}
   */
  async enableAccount(username) {
    await this.post('/account/enable', { username })
  }

  /**
   * @param {AuditListOptions} opts
   * @returns {Promise<AuditPage>}
   */
  async listAuditLog(opts) {
    return this.postJSON('/audit/log', opts)
  }

  // --- Upgrades ---

  /** @returns {Promise<PackageUpgrade[]>} */
  async listUpgrades() {
    return this.getJSON('/packages/upgrades')
  }

  /** @returns {Promise<void>} */
  async dismissUpgrades() {
    await this.post('/packages/upgrades/dismiss', {})
  }

  // --- Archives ---

  /**
   * Upload an archive to a subvolume.
   * Uses FormData with fetch() directly (not this.post()) so the browser
   * sets the multipart boundary automatically.
   * @param {string} subvolume
   * @param {File} file
   * @returns {Promise<{needs_restart: boolean, message: string}>}
   */
  async uploadArchive(subvolume, file) {
    const form = new FormData()
    form.append('subvolume', subvolume)
    form.append('archive', file)

    /** @type {HeadersInit} */
    const headers = {}
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    const resp = await fetch(`${this.baseURL}/storage/upload-archive`, {
      method: 'POST',
      headers,
      body: form,
    })
    if (resp.status !== 200) {
      const text = await resp.text().catch(() => '')
      throw new ApiError('POST', '/storage/upload-archive', resp.status, text)
    }
    return resp.json()
  }

  /**
   * Download an archive of the specified subvolume.
   * Returns the raw Response so the caller can stream resp.body to disk.
   * @param {string} subvolume
   * @param {string[]} [paths]
   * @param {string} [stopService]
   * @returns {Promise<Response>}
   */
  async downloadArchive(subvolume, paths, stopService) {
    /** @type {HeadersInit} */
    const headers = { 'Content-Type': 'application/json' }
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    const body = { subvolume }
    if (paths && paths.length > 0) body.paths = paths
    if (stopService) body.stop_service = stopService

    const resp = await fetch(`${this.baseURL}/storage/download-archive`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    })
    if (resp.status !== 200) {
      const text = await resp.text().catch(() => '')
      throw new ApiError('POST', '/storage/download-archive', resp.status, text)
    }
    return resp
  }

  // --- Settings (admin) ---

  /** @returns {Promise<Record<string, string>>} */
  async getSettings() {
    return this.getJSON('/settings')
  }

  /**
   * @param {string} key
   * @returns {Promise<string>}
   */
  async getSetting(key) {
    /** @type {{ value: string }} */
    const result = await this.postJSON('/settings/get', { key })
    return result.value
  }

  /**
   * @param {string} key
   * @param {string} value
   * @returns {Promise<void>}
   */
  async setSetting(key, value) {
    await this.post('/settings/set', { key, value })
  }
}
