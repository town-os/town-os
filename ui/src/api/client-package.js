/** @import { Question, Responses, InstalledInfo } from './types.js' */
import { ApiError, SystemControllerClient } from './core.js'

/**
 * List available packages across all repositories with pagination.
 * @param {string} [sortBy] - Field to sort by.
 * @param {string} [sortOrder] - Sort direction: "asc" or "desc".
 * @param {number} [limit] - Maximum entries per page (default 20).
 * @param {number} [offset] - Number of entries to skip for pagination.
 * @param {string} [search] - Case-insensitive substring to match across string fields.
 * @returns {Promise<{entries: string[], has_more: boolean, total_pages: number}>}
 */
SystemControllerClient.prototype.listPackages = async function (sortBy, sortOrder, limit, offset, search) {
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
 * List packages grouped by their source repository.
 * @param {string} [search] - Case-insensitive substring to filter packages.
 * @returns {Promise<Array<{repo: string, packages: Array<{repo: string, name: string, version: string}>}>>}
 */
SystemControllerClient.prototype.listPackagesByRepo = async function (search) {
  const params = new URLSearchParams()
  if (search) params.set('search', search)
  const qs = params.toString()
  return this.getJSON(`/packages/by-repo${qs ? `?${qs}` : ''}`)
}

/**
 * List installed packages with pagination. Entries use the format
 * "repo/name@version" (e.g. "myrepo/nginx@1.0.0").
 * @param {string} [sortBy] - Field to sort by.
 * @param {string} [sortOrder] - Sort direction: "asc" or "desc".
 * @param {number} [limit] - Maximum entries per page (default 20).
 * @param {number} [offset] - Number of entries to skip for pagination.
 * @param {string} [search] - Case-insensitive substring to match.
 * @returns {Promise<{entries: string[], has_more: boolean, total_pages: number}>}
 */
SystemControllerClient.prototype.listInstalled = async function (sortBy, sortOrder, limit, offset, search) {
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
 * @param {string} repo
 * @param {string} name
 * @param {string} version
 * @returns {Promise<Responses>}
 */
SystemControllerClient.prototype.getResponses = async function (repo, name, version) {
  return this.postJSON('/packages/responses', { repo, name, version })
}

/**
 * @param {string} repo
 * @param {string} name
 * @param {string} version
 * @returns {Promise<InstalledInfo>}
 */
SystemControllerClient.prototype.getInstalledInfo = async function (repo, name, version) {
  return this.postJSON('/packages/installed/info', { repo, name, version })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<Responses>}
 */
SystemControllerClient.prototype.getLastResponses = async function (repo, name) {
  return this.postJSON('/packages/last-responses', { repo, name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.clearLastResponses = async function (repo, name) {
  await this.post('/packages/clear-last-responses', { repo, name })
}

/**
 * @param {string} name
 * @returns {Promise<Record<string, Question>>}
 */
SystemControllerClient.prototype.getPackageQuestions = async function (name) {
  return this.postJSON('/packages/questions', { name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @param {string} version
 * @returns {Promise<Record<string, Question>>}
 */
SystemControllerClient.prototype.getPackageQuestionsByIdentity = async function (repo, name, version) {
  return this.postJSON('/packages/questions/identity', { repo, name, version })
}

/**
 * @param {string} name
 * @returns {Promise<string[]>}
 */
SystemControllerClient.prototype.listPackageVersions = async function (name) {
  return this.postJSON('/packages/versions', { name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<{has_uninstalled_volumes: boolean, uninstalled_versions?: string[], installed_versions?: string[]}>}
 */
SystemControllerClient.prototype.listUninstalledVolumes = async function (repo, name) {
  return this.postJSON('/packages/uninstalled-volumes', { repo, name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.purgeUninstalledVolumes = async function (repo, name) {
  await this.post('/packages/purge-uninstalled-volumes', { repo, name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @param {string} version
 * @returns {Promise<object>}
 */
SystemControllerClient.prototype.installPreview = async function (repo, name, version) {
  return this.postJSON('/packages/install-preview', { repo, name, version })
}

/**
 * Install a package at the given version.
 * @param {string} repo - Source repository name.
 * @param {string} name - Package name.
 * @param {string} version - Version to install.
 * @param {Responses} responses - Configuration answers as a map of question keys to string values.
 * @param {boolean} [reuseVolumes=false] - When true, preserve existing data volumes from a prior installation.
 * @param {string} [importFromVersion] - When provided, import data from this prior installed version.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.installPackage = async function (repo, name, version, responses, reuseVolumes = false, importFromVersion) {
  const body = { repo, name, version, responses }
  if (reuseVolumes) body.reuse_volumes = true
  if (importFromVersion) body.import_from_version = importFromVersion
  await this.post('/packages/install', body)
}

/**
 * Uninstall a package.
 * @param {string} repo - Source repository name.
 * @param {string} name - Package name.
 * @param {string} version - Installed version.
 * @param {boolean} [purgeVolumes=false] - When true, also delete all associated data volumes.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.uninstallPackage = async function (repo, name, version, purgeVolumes = false) {
  await this.post('/packages/uninstall', { repo, name, version, purge_volumes: purgeVolumes })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.purgeVolumes = async function (repo, name) {
  await this.post('/packages/purge-volumes', { repo, name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.disablePackage = async function (repo, name) {
  await this.post('/packages/disable', { repo, name })
}

/**
 * @param {string} repo
 * @param {string} name
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.enablePackage = async function (repo, name) {
  await this.post('/packages/enable', { repo, name })
}
