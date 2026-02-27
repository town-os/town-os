import { ApiError, SystemControllerClient } from './core.js'

/**
 * Upload an archive to a subvolume. Supported formats are tar.gz, tar.bz2,
 * and tar.xz (detected from the filename extension). Uses FormData with
 * fetch() directly (not this.post()) so the browser sets the multipart
 * boundary automatically.
 * @param {string} subvolume - Target subvolume name.
 * @param {File} file - Archive file to upload.
 * @param {string} [subpath] - When provided, extract into this subdirectory within the subvolume.
 * @param {string} [stopService] - When provided, stop this systemd service before extraction and restart afterward.
 * @returns {Promise<{needs_restart: boolean, message: string}>}
 */
SystemControllerClient.prototype.uploadArchive = async function (subvolume, file, subpath, stopService) {
  const form = new FormData()
  form.append('subvolume', subvolume)
  if (subpath) form.append('subpath', subpath)
  if (stopService) form.append('stop_service', stopService)
  form.append('archive', file)

  /** @type {HeadersInit} */
  const headers = {}
  if (this.token) {
    headers['Authorization'] = `Bearer ${this.token}`
  }

  const resp = await fetch(`${this.baseURL}/storage/upload-archive`, {
    method: 'POST',
    headers,
    body: form,
  })
  if (resp.status !== 200) {
    const text = await resp.text().catch(() => '')
    throw new ApiError('POST', '/storage/upload-archive', resp.status, text)
  }
  return resp.json()
}

/**
 * Download an archive of the specified subvolume.
 * Returns the raw Response so the caller can stream resp.body to disk.
 * @param {string} subvolume - Source subvolume name.
 * @param {string[]} [paths] - Paths within the subvolume to include; omit to include everything.
 * @param {string} [stopService] - When provided, stop this systemd service during archival.
 * @param {string} [format] - Compression format: "tar.gz" (default), "tar.bz2", "tar.xz"
 * @returns {Promise<Response>}
 */
SystemControllerClient.prototype.downloadArchive = async function (subvolume, paths, stopService, format) {
  /** @type {HeadersInit} */
  const headers = { 'Content-Type': 'application/json' }
  if (this.token) {
    headers['Authorization'] = `Bearer ${this.token}`
  }

  const body = { subvolume }
  if (paths && paths.length > 0) body.paths = paths
  if (stopService) body.stop_service = stopService
  if (format) body.format = format

  const resp = await fetch(`${this.baseURL}/storage/download-archive`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (resp.status !== 200) {
    const text = await resp.text().catch(() => '')
    throw new ApiError('POST', '/storage/download-archive', resp.status, text)
  }
  return resp
}
