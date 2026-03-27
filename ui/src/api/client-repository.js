/** @import { RepositoryInfo } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Register a new package repository.
 * @param {string} name - Display name for the repository.
 * @param {string} url - Git URL of the package repository.
 * @param {string} [username] - Username for repository authentication (optional).
 * @param {string} [password] - Password for repository authentication (optional).
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.addRepository = async function (name, url, username = '', password = '') {
  await this.post('/repository/add', { name, url, username, password })
}

/**
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeRepository = async function (name) {
  await this.post('/repository/remove', { name })
}

/**
 * Change the priority position of a repository. Position 0 is highest priority.
 * @param {string} name - Repository name.
 * @param {number} position - New zero-based priority position.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.moveRepository = async function (name, position) {
  await this.post('/repository/move', { name, position })
}

/** @returns {Promise<void>} */
SystemControllerClient.prototype.refreshRepositories = async function () {
  const resp = await this.post('/repository/refresh', {})
  // Drain the SSE stream so the request completes.
  await resp.text().catch(() => {})
}

/**
 * Refresh repositories and return the raw SSE response for progress streaming.
 * @returns {Promise<Response>}
 */
SystemControllerClient.prototype.refreshRepositoriesStream = async function () {
  return this.post('/repository/refresh', {})
}

/**
 * List configured package repositories with pagination.
 * @param {string} [sortBy] - Field to sort by (e.g. "name", "url").
 * @param {string} [sortOrder] - Sort direction: "asc" or "desc".
 * @param {number} [limit] - Maximum entries per page (default 20).
 * @param {number} [offset] - Number of entries to skip for pagination.
 * @param {string} [search] - Case-insensitive substring to match across string fields.
 * @returns {Promise<{entries: RepositoryInfo[], has_more: boolean, total_pages: number}>}
 */
SystemControllerClient.prototype.listRepositories = async function (sortBy, sortOrder, limit, offset, search) {
  const params = new URLSearchParams()
  if (sortBy) params.set('sort_by', sortBy)
  if (sortOrder) params.set('sort_order', sortOrder)
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (search) params.set('search', search)
  const qs = params.toString()
  return this.getJSON(`/repository${qs ? `?${qs}` : ''}`)
}
