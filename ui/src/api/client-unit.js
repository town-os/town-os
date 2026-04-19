/** @import { UnitStatus, JournalEntry, StatusAction } from './types.js' */
import { ApiError, SystemControllerClient } from './core.js'

/**
 * @param {string} [sortBy]
 * @param {string} [sortOrder]
 * @param {number} [limit]
 * @param {number} [offset]
 * @param {string} [search]
 * @returns {Promise<{entries: UnitStatus[], has_more: boolean, total_pages: number}>}
 */
SystemControllerClient.prototype.listUnits = async function (sortBy, sortOrder, limit, offset, search) {
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
SystemControllerClient.prototype.logReplay = async function* (unit) {
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
 * Fetch a page of journal log entries for a unit with cursor-based pagination.
 * @param {string} unit - Systemd unit name.
 * @param {number} [lines=100] - Maximum number of entries to return.
 * @param {string} [beforeCursor] - Return entries before this opaque cursor (for backward paging).
 * @param {string} [afterCursor] - Return entries after this opaque cursor (for forward paging).
 * @param {string} [grep] - Filter to entries whose message contains this substring.
 * @param {number} [since] - Only entries at or after this Unix timestamp (seconds).
 * @param {number} [until] - Only entries at or before this Unix timestamp (seconds).
 * @param {number} [priority] - Maximum syslog priority level to include; 0 disables filtering,
 *   values 1–7 include entries with priority <= the value (e.g. 3 returns emergency, alert,
 *   critical, and error).
 * @returns {Promise<{entries: JournalEntry[], cursor: string, end_cursor: string}>}
 */
SystemControllerClient.prototype.logTail = async function (unit, lines = 100, beforeCursor, afterCursor, grep, since, until, priority) {
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
 * Apply a status action to a systemd unit.
 * @param {string} name - Systemd unit name.
 * @param {StatusAction} action - Action to apply: "start", "stop", or "restart".
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setUnitStatus = async function (name, action) {
  await this.post('/systemd/status', { name, action })
}

/**
 * Fetch package service units grouped into a dependency tree.
 * @param {string} [sortBy]
 * @param {string} [sortOrder]
 * @param {number} [limit]
 * @param {number} [offset]
 * @param {string} [search]
 * @returns {Promise<{entries: any[], has_more: boolean, total_pages: number, total_count: number}>}
 */
SystemControllerClient.prototype.listUnitsTree = async function (sortBy, sortOrder, limit, offset, search) {
  const params = new URLSearchParams()
  if (sortBy) params.set('sort_by', sortBy)
  if (sortOrder) params.set('sort_order', sortOrder)
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (search) params.set('search', search)
  const qs = params.toString()
  return this.getJSON(`/systemd/units-tree${qs ? `?${qs}` : ''}`)
}

/**
 * Cascade a status action across a package and all its installed dependencies.
 * @param {string} repo
 * @param {string} name - Raw effective package name (may contain "--dep--").
 * @param {string} version
 * @param {StatusAction} action - "start", "stop", or "restart".
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.setUnitStatusTree = async function (repo, name, version, action) {
  await this.post('/systemd/status/tree', { repo, name, version, action })
}

/**
 * Stream historical journal entries for every systemd unit in a package's
 * dependency tree as a single SSE stream. Entries from the parent and
 * every dependency arrive interleaved in chronological order.
 * @param {string} repo
 * @param {string} name - Raw effective package name (may contain "--dep--").
 * @param {string} version
 * @returns {AsyncGenerator<JournalEntry>}
 */
SystemControllerClient.prototype.logReplayTree = async function* (repo, name, version) {
  /** @type {HeadersInit} */
  const headers = {}
  if (this.token) {
    headers['Authorization'] = `Bearer ${this.token}`
  }
  const params = new URLSearchParams({ repo, name, version })
  const resp = await fetch(`${this.baseURL}/systemd/logs/tree?${params.toString()}`, { headers })
  if (resp.status !== 200) {
    const text = await resp.text().catch(() => '')
    throw new ApiError('GET', '/systemd/logs/tree', resp.status, text)
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
 * Fetch a page of journal entries covering every systemd unit in a
 * package's dependency tree. Filters and cursors behave identically to
 * logTail; the unit set is derived server-side from the install records.
 * @param {string} repo
 * @param {string} name - Raw effective package name (may contain "--dep--").
 * @param {string} version
 * @param {number} [lines=100]
 * @param {string} [beforeCursor]
 * @param {string} [afterCursor]
 * @param {string} [grep]
 * @param {number} [since]
 * @param {number} [until]
 * @param {number} [priority]
 * @returns {Promise<{entries: JournalEntry[], cursor: string, end_cursor: string}>}
 */
SystemControllerClient.prototype.logTailTree = async function (repo, name, version, lines = 100, beforeCursor, afterCursor, grep, since, until, priority) {
  const params = new URLSearchParams({ repo, name, version, lines: String(lines) })
  if (beforeCursor) params.set('before', beforeCursor)
  if (afterCursor) params.set('after', afterCursor)
  if (grep) params.set('grep', grep)
  if (since) params.set('since', String(since))
  if (until) params.set('until', String(until))
  if (priority) params.set('priority', String(priority))
  return this.getJSON(`/systemd/logs/tree/tail?${params.toString()}`)
}
