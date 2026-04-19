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
 * Modify an existing filesystem's name or quota.
 *
 * Renaming is only allowed for user filesystems. Package volumes (those with
 * an installed/ or uninstalled/ prefix) cannot be renamed; the Control Plane
 * Service will reject the request if fs.name differs from the original name.
 *
 * @param {string} name - Current full filesystem name (e.g. "myvolume" for user
 *   volumes, or "installed/repo/pkg/1.0/data" for package volumes).
 * @param {Filesystem} fs - Desired state. fs.name is the new name (must equal
 *   name for package volumes). fs.quota is the new quota in bytes (0 = unlimited).
 * @returns {Promise<void>}
 *
 * Calls POST /storage/modify on the Control Plane Service.
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

/**
 * List package volumes grouped by package.
 * @param {boolean} includeUninstalled - Whether to include uninstalled volumes.
 * @returns {Promise<Array<{package: string, repo: string, volumes: Array<{name: string, internal_name: string, repo: string, quota: number, state: string}>}>>}
 */
SystemControllerClient.prototype.listPackageVolumes = async function (includeUninstalled) {
  return this.postJSON('/storage/package-volumes', { include_uninstalled: !!includeUninstalled })
}

/**
 * Remove a package volume by its internal name.
 * @param {string} internalName - Full internal path (e.g. "installed/repo/pkg/1.0/data").
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removePackageVolume = async function (internalName) {
  await this.post('/storage/remove-package-volume', { internal_name: internalName })
}

/**
 * Cascade-remove every volume under a package (and optionally a single
 * version). The server stops every service unit in the package's dependency
 * tree before the btrfs delete, so a container holding a mount cannot race
 * the delete.
 *
 * @param {Object} opts
 * @param {string} opts.repo - Repository name (e.g. "core").
 * @param {string} opts.name - Effective package name (may contain "--dep--").
 * @param {string} [opts.version] - Version to scope the delete to; omit to
 *     delete every installed version of (repo, name).
 * @param {boolean} [opts.includeUninstalled] - Also sweep the matching
 *     uninstalled/ subtree. Defaults to false so a cascade invoked while
 *     "Show uninstalled" is off does not nuke archived volumes the UI
 *     isn't currently showing.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.removePackageVolumeGroup = async function ({ repo, name, version, includeUninstalled }) {
  await this.post('/storage/remove-package-volume-group', {
    repo,
    name,
    version: version || '',
    include_uninstalled: !!includeUninstalled,
  })
}
