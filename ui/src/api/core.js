/**
 * Error thrown by {@link SystemControllerClient} when the API returns a
 * non-200 status. The detail property contains a human-readable message
 * extracted from the response body (RFC 9457 problem+json or legacy format).
 * The problem property contains the full parsed problem detail object when
 * available.
 */
export class ApiError extends Error {
  /**
   * @param {string} method - HTTP method (e.g. "GET", "POST").
   * @param {string} path - API path that was called.
   * @param {number} status - HTTP status code.
   * @param {string} body - Raw response body text.
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

/**
 * HTTP client for the Control Plane Service API. All methods throw
 * {@link ApiError} on non-200 responses. Methods that require authentication
 * use the token set via {@link SystemControllerClient#setToken}. Methods
 * that accept explicit token parameters (listSessions, sessionUsername)
 * pass the token directly in the Authorization header.
 */
export class SystemControllerClient {
  /**
   * Create a new client.
   * @param {string} baseURL - Base URL of the Control Plane Service (trailing slashes are stripped).
   */
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
}
