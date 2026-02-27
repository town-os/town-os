import { SystemControllerClient } from './core.js'

/**
 * Create a new page site.
 * @param {string} name - Unique name for the page.
 * @param {string} repoURL - Git repository URL.
 * @param {string} [branch] - Branch name (defaults to "main").
 * @param {string} [domain] - Domain name (defaults to page name).
 * @returns {Promise<object>}
 */
SystemControllerClient.prototype.createPage = async function (name, repoURL, branch, domain) {
  const body = { name, repo_url: repoURL }
  if (branch) body.branch = branch
  if (domain) body.domain = domain
  return this.postJSON('/pages/create', body)
}

/**
 * Update an existing page.
 * @param {string} name - Page name.
 * @param {object} updates - Fields to update (repo_url, branch, domain).
 * @returns {Promise<object>}
 */
SystemControllerClient.prototype.updatePage = async function (name, updates) {
  return this.postJSON('/pages/update', { name, ...updates })
}

/**
 * Remove a page and its cloned repository.
 * @param {string} name - Page name.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removePage = async function (name) {
  await this.post('/pages/remove', { name })
}

/**
 * Rebuild a page by pulling latest changes.
 * @param {string} name - Page name.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.rebuildPage = async function (name) {
  await this.post('/pages/rebuild', { name })
}

/**
 * List pages with sorting, search, and pagination.
 * @param {string} [sortBy] - Field to sort by.
 * @param {string} [sortOrder] - Sort direction: "asc" or "desc".
 * @param {number} [limit] - Maximum entries per page.
 * @param {number} [offset] - Number of entries to skip.
 * @param {string} [search] - Substring to match.
 * @returns {Promise<{entries: object[], has_more: boolean, total_pages: number, total_count: number}>}
 */
SystemControllerClient.prototype.listPages = async function (sortBy, sortOrder, limit, offset, search) {
  const params = new URLSearchParams()
  if (sortBy) params.set('sort_by', sortBy)
  if (sortOrder) params.set('sort_order', sortOrder)
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (search) params.set('search', search)
  const qs = params.toString()
  return this.getJSON(`/pages${qs ? `?${qs}` : ''}`)
}
