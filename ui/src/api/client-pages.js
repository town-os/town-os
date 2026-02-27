/** @import { PageSite, PageSiteUpdate } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Create a new page site for static content hosting via git repository.
 *
 * @param {string} name - Unique name for the page site (required).
 * @param {string} repoURL - Git repository URL containing static content (required).
 * @param {string} branch - Git branch to serve. Defaults to "main" on the server if empty.
 * @param {string} [domain] - Custom domain for the page. Defaults to the page name if empty.
 * @returns {Promise<PageSite>}
 *
 * Calls POST /pages/create on the Control Plane Service.
 */
SystemControllerClient.prototype.createPage = async function (name, repoURL, branch, domain) {
  return this.postJSON('/pages/create', { name, repo_url: repoURL, branch, domain: domain || '' })
}

/**
 * Update fields on an existing page site. Only provided fields are changed.
 *
 * @param {string} name - Page site name to update (required).
 * @param {PageSiteUpdate} fields - Partial update: repo_url, branch, domain, status.
 *   Only non-undefined fields are applied.
 * @returns {Promise<PageSite>}
 *
 * Calls POST /pages/update on the Control Plane Service.
 */
SystemControllerClient.prototype.updatePage = async function (name, fields) {
  return this.postJSON('/pages/update', { name, fields })
}

/**
 * Delete a page site and its cloned repository.
 *
 * @param {string} name - Page site name to remove (required).
 * @returns {Promise<void>}
 *
 * Calls POST /pages/remove on the Control Plane Service.
 */
SystemControllerClient.prototype.removePage = async function (name) {
  await this.post('/pages/remove', { name })
}

/**
 * List page sites with pagination, sorting, and search.
 *
 * @param {string} [sortBy] - Field to sort by (e.g. "name", "domain", "status"). Default: server default (name).
 * @param {string} [sortOrder] - Sort direction: "asc" (default) or "desc".
 * @param {number} [limit] - Maximum entries per page. Default: server default (20).
 * @param {number} [offset] - Number of entries to skip for pagination. Default: 0.
 * @param {string} [search] - Case-insensitive search filter across all fields.
 * @returns {Promise<{entries: PageSite[], has_more: boolean, total_pages: number, total_count: number}>}
 *
 * Calls GET /pages on the Control Plane Service.
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

/**
 * Rebuild a page by pulling the latest content from its git repository.
 *
 * @param {string} name - Page site name to rebuild (required).
 * @returns {Promise<PageSite>}
 *
 * Calls POST /pages/rebuild on the Control Plane Service.
 */
SystemControllerClient.prototype.rebuildPage = async function (name) {
  return this.postJSON('/pages/rebuild', { name })
}
