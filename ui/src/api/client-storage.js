/** @import { Filesystem } from './types.js' */
import { SystemControllerClient } from './core.js'

/**
 * @param {Filesystem} fs
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.createFilesystem = async function (fs) {
  await this.post('/storage/create', fs)
}

/**
 * @param {string} name
 * @param {Filesystem} fs
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.modifyFilesystem = async function (name, fs) {
  await this.post('/storage/modify', { name, filesystem: fs })
}

/**
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removeFilesystem = async function (name) {
  await this.post('/storage/remove', { name })
}

/**
 * List filesystems with optional filtering and pagination.
 * @param {string} prefix - Filter to filesystems whose name starts with this string; empty matches all.
 * @param {string} [sortBy] - Field to sort by (e.g. "name", "quota").
 * @param {string} [sortOrder] - Sort direction: "asc" or "desc".
 * @param {string} [state] - Filter by state: "user", "installed", or "uninstalled"; omit for all.
 * @param {number} [limit] - Maximum entries per page (default 20).
 * @param {number} [offset] - Number of entries to skip for pagination.
 * @param {string} [search] - Case-insensitive substring to match across string fields.
 * @returns {Promise<{entries: Filesystem[], has_more: boolean, total_pages: number, total_count: number}>}
 */
SystemControllerClient.prototype.listFilesystems = async function (prefix, sortBy, sortOrder, state, limit, offset, search) {
  const body = { name: prefix }
  if (sortBy) body.sort_by = sortBy
  if (sortOrder) body.sort_order = sortOrder
  if (state) body.state = state
  if (limit) body.limit = limit
  if (offset) body.offset = offset
  if (search) body.search = search
  return this.postJSON('/storage', body)
}
