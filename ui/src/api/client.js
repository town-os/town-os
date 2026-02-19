/** @import { Filesystem, UnitStatus, JournalEntry, StatusAction, Account, UpdateFields, Session, AuditListOptions, AuditPage, Question, Responses, RepositoryInfo, PingResponse, AuthenticateResponse } from './types.js' */

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
      throw new Error(`POST ${path}: status ${resp.status}: ${text}`)
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
      throw new Error(`GET ${path}: status ${resp.status}: ${text}`)
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
   * @returns {Promise<Filesystem[]>}
   */
  async listFilesystems(prefix) {
    return this.postJSON('/storage', { name: prefix })
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

  /** @returns {Promise<RepositoryInfo[]>} */
  async listRepositories() {
    return this.getJSON('/repository')
  }

  /** @returns {Promise<string[]>} */
  async listPackages() {
    return this.getJSON('/packages')
  }

  /** @returns {Promise<string[]>} */
  async listInstalled() {
    return this.getJSON('/packages/installed')
  }

  /**
   * @param {string} name
   * @param {string} version
   * @returns {Promise<Responses>}
   */
  async getResponses(name, version) {
    return this.postJSON('/packages/responses', { name, version })
  }

  /** @returns {Promise<UnitStatus[]>} */
  async listUnits() {
    return this.getJSON('/systemd/units')
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
      throw new Error(`GET /systemd/logs: status ${resp.status}: ${text}`)
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

  /** @returns {Promise<Account[]>} */
  async listAccounts() {
    return this.getJSON('/account')
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
