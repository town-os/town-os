/** @import { Question, Responses, InstalledInfo } from './types.js' */
import { ApiError, SystemControllerClient } from './core.js'

/**
 * List available packages across all repositories with pagination.
 * Calls GET /packages on the Control Plane Service.
 * @param {string} [sortBy] - Field to sort by (e.g. "name", "repo", "version").
 * @param {string} [sortOrder] - Sort direction: "asc" (default) or "desc".
 * @param {number} [limit] - Maximum entries per page (default 20).
 * @param {number} [offset] - Number of entries to skip for pagination.
 * @param {string} [search] - Case-insensitive substring to match across string fields.
 * @param {boolean} [installedOnly] - When true, only return packages that are currently installed. Default false.
 * @returns {Promise<{entries: string[], has_more: boolean, total_pages: number, total_count: number}>}
 */
SystemControllerClient.prototype.listPackages = async function (sortBy, sortOrder, limit, offset, search, installedOnly, featuredOnly) {
  const params = new URLSearchParams()
  if (sortBy) params.set('sort_by', sortBy)
  if (sortOrder) params.set('sort_order', sortOrder)
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (search) params.set('search', search)
  if (installedOnly) params.set('installed_only', 'true')
  if (featuredOnly) params.set('featured_only', 'true')
  const qs = params.toString()
  return this.getJSON(`/packages${qs ? `?${qs}` : ''}`)
}

/**
 * List packages grouped by their source repository.
 * Calls GET /packages/by-repo on the Control Plane Service.
 * @param {string} [search] - Case-insensitive substring to filter packages by name.
 * @returns {Promise<Array<{repo: string, packages: Array<{repo: string, name: string, version: string}>, featured?: string[]}>>}
 *   Each group includes an optional `featured` array of package names marked as featured in that repository.
 */
SystemControllerClient.prototype.listPackagesByRepo = async function (search, featuredOnly) {
  const params = new URLSearchParams()
  if (search) params.set('search', search)
  if (featuredOnly) params.set('featured_only', 'true')
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

/**
 * List cached VM disk images in the vm-images subvolume.
 * Calls GET /vm-images on the Control Plane Service.
 * @returns {Promise<Array<{name: string, size: number}>>}
 */
SystemControllerClient.prototype.listVMImages = async function () {
  return this.getJSON('/vm-images')
}

/**
 * Upload (download + convert) a VM image from a remote URL.
 * The image is cached locally in raw format.
 * Calls POST /vm-images/upload on the Control Plane Service.
 * @param {string} url - Remote URL to download the VM image from.
 * @param {string} [name] - Optional filename for the cached image.
 * @returns {Promise<{name: string}>}
 */
SystemControllerClient.prototype.uploadVMImage = async function (url, name) {
  const body = { url }
  if (name) body.name = name
  return this.postJSON('/vm-images/upload', body)
}

/**
 * Delete a cached VM image from the vm-images subvolume.
 * Calls POST /vm-images/delete on the Control Plane Service.
 * @param {string} name - Filename of the VM image to delete.
 * @returns {Promise<void>}
 */
SystemControllerClient.prototype.deleteVMImage = async function (name) {
  await this.post('/vm-images/delete', { name })
}
