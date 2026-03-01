import { Badge } from '@/components/ui/badge'

const UNITS = {
  B: 1,
  MB: 1024 * 1024,
  GB: 1024 * 1024 * 1024,
  TB: 1024 * 1024 * 1024 * 1024,
}

function formatQuotaText(bytes) {
  if (!bytes || bytes === 0) return 'none'
  if (bytes >= UNITS.TB) return `${(bytes / UNITS.TB).toFixed(2)} TB`
  if (bytes >= UNITS.GB) return `${(bytes / UNITS.GB).toFixed(2)} GB`
  if (bytes >= UNITS.MB) return `${(bytes / UNITS.MB).toFixed(2)} MB`
  return `${bytes} B`
}

function formatQuota(bytes) {
  if (!bytes || bytes === 0) return <Badge className="bg-black text-white hover:bg-black/90">none</Badge>
  return formatQuotaText(bytes)
}

/** Decompose a byte count into a [value, unit] pair for the form. */
function decomposeQuota(bytes) {
  if (!bytes || bytes === 0) return ['', 'GB']
  if (bytes >= UNITS.TB && bytes % UNITS.TB === 0) return [bytes / UNITS.TB, 'TB']
  if (bytes >= UNITS.GB && bytes % UNITS.GB === 0) return [bytes / UNITS.GB, 'GB']
  if (bytes >= UNITS.MB && bytes % UNITS.MB === 0) return [bytes / UNITS.MB, 'MB']
  return [bytes, 'B']
}

/**
 * Derive the systemd service unit name from a volume display name.
 * e.g. "repo/name/version/volName" -> "town-os-package--repo-name-version.service"
 */
function deriveServiceName(volumeName) {
  const parts = volumeName.split('/')
  if (parts.length < 3) return ''
  return `town-os-package--${parts[0]}-${parts[1]}-${parts[2]}.service`
}

export { UNITS, formatQuotaText, formatQuota, decomposeQuota, deriveServiceName }
