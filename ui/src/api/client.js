/** @import { Filesystem, UnitStatus, JournalEntry, StatusAction, Account, UpdateFields, Session, AuditListOptions, AuditPage, Question, Responses, RepositoryInfo, PingResponse, AuthenticateResponse } from './types.js' */

export class ApiError extends Error {
  /**
   * @param {string} method
   * @param {string} path
   * @param {number} status
   * @param {string} body
   */
  constructor(method, path, status, body) {
    super(`${method} ${path}: status ${status}: ${body}`)
    this.name = 'ApiError'
    this.status = status
    this.path = path
    this.body = body
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
   * @returns {Promise<Filesystem[]>}
   */
  async listFilesystems(prefix, sortBy, sortOrder) {
    const body = { name: prefix }
    if (sortBy) body.sort_by = sortBy
    if (sortOrder) body.sort_order = sortOrder
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
   * @param {string} name
   * @param {string} version
   * @returns {Promise<Responses>}
   */
  async getResponses(name, version) {
    return this.postJSON('/packages/responses', { name, version })
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
   * @returns {Promise<{entries: JournalEntry[], cursor: string, end_cursor: string}>}
   */
  async logTail(unit, lines = 100, beforeCursor, afterCursor, grep, since, until) {
    const params = new URLSearchParams({ unit, lines: String(lines) })
    if (beforeCursor) params.set('before', beforeCursor)
    if (afterCursor) params.set('after', afterCursor)
    if (grep) params.set('grep', grep)
    if (since) params.set('since', String(since))
    if (until) params.set('until', String(until))
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
   * @returns {Promise<Account[]>}
   */
  async listAccounts(sortBy, sortOrder) {
    const params = new URLSearchParams()
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
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
   * @param {string} name
   * @param {string} version
   * @param {Responses} responses
   * @returns {Promise<void>}
   */
  async installPackage(name, version, responses) {
    await this.post('/packages/install', { name, version, responses })
  }

  /**
   * @param {string} name
   * @param {string} version
   * @returns {Promise<void>}
   */
  async uninstallPackage(name, version) {
    await this.post('/packages/uninstall', { name, version })
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
   * @param {string} username
   * @returns {Promise<void>}
   */
  async disableAccount(username) {
    await this.post('/account/disable', { username })
  }

  /**
   * @param {AuditListOptions} opts
   * @returns {Promise<AuditPage>}
   */
  async listAuditLog(opts) {
    return this.postJSON('/audit/log', opts)
  }
}
