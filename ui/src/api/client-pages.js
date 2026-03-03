/** @import { PageSite, PageSiteUpdate } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * Create a new page site for static content hosting.
 *
 * @param {string} name - Unique name for the page site (required).
 * @param {string} repoURL - Git repository URL containing static content (required for source_type "git").
 * @param {string} branch - Git branch to serve. Defaults to "main" on the server if empty.
 * @param {string} [domain] - Custom domain for the page. Defaults to the page name if empty.
 * @param {string} [sourceType] - Content source: "archive" (default), "container_image", or "git".
 * @param {string} [image] - Container image reference (required for source_type "container_image").
 * @param {string} [imageDirectory] - Directory within the container image to extract (required for source_type "container_image").
 * @returns {Promise<PageSite>}
 *
 * Calls POST /pages/create on the Control Plane Service.
 */
SystemControllerClient.prototype.createPage = async function (name, repoURL, branch, domain, sourceType, image, imageDirectory) {
  return this.postJSON('/pages/create', {
    name,
    repo_url: repoURL || '',
    branch: branch || '',
    domain: domain || '',
    source_type: sourceType || 'archive',
    image: image || '',
    image_directory: imageDirectory || '',
  })
}

/**
 * Update fields on an existing page site. Only provided fields are changed.
 *
 * @param {string} name - Page site name to update (required).
 * @param {PageSiteUpdate} fields - Partial update: repo_url, branch, domain, source_type,
 *   image, image_directory, status. Only non-undefined fields are applied.
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
 * @param {string} [sortBy] - Field to sort by (e.g. "name", "domain", "status", "source_type"). Default: server default (name).
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
 * Rebuild a page by pulling the latest content from its git repository
 * (git pages), re-extracting from the container image (container_image pages),
 * or returning an error for archive pages (use uploadPageArchive instead).
 *
 * @param {string} name - Page site name to rebuild (required).
 * @returns {Promise<PageSite>}
 *
 * Calls POST /pages/rebuild on the Control Plane Service.
 */
SystemControllerClient.prototype.rebuildPage = async function (name) {
  return this.postJSON('/pages/rebuild', { name })
}

/**
 * Upload and extract an archive for an archive-type page. The page transitions
 * to "active" status on success or "error" on failure. Only valid for pages
 * with source_type "archive".
 *
 * @param {string} name - Page name to upload content for (required).
 * @param {File} file - Archive file. Supported formats: .tar, .tar.gz/.tgz,
 *   .tar.bz2/.tbz2, .tar.xz/.txz.
 * @returns {Promise<PageSite>}
 *
 * Calls POST /pages/upload on the Control Plane Service.
 */
SystemControllerClient.prototype.uploadPageArchive = async function (name, file) {
  const form = new FormData()
  form.append('name', name)
  form.append('archive', file)

  const resp = await fetch(`${this.baseURL}/pages/upload`, {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${this.token}` },
    body: form,
  })

  if (!resp.ok) {
    const text = await resp.text()
    let detail = text
    try {
      const parsed = JSON.parse(text)
      detail = parsed.detail || parsed.message || text
    } catch { /* use raw text */ }
    const err = new Error(detail)
    err.detail = detail
    throw err
  }

  return resp.json()
}
