import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'

const DEFAULT_QUOTA_KEY = 'default_quota'
const DEFAULT_QUOTA_BYTES = 50 * 1024 * 1024 * 1024 // 50 GB
const MAX_ARCHIVE_SIZE_KEY = 'max_archive_size'
const DEFAULT_MAX_ARCHIVE_SIZE_BYTES = 20 * 1024 * 1024 // 20 MB
const ARCHIVE_UNPACK_TIMEOUT_KEY = 'archive_unpack_timeout'
const DEFAULT_ARCHIVE_UNPACK_TIMEOUT = 120 // seconds

function formatBytes(bytes) {
  if (bytes === 0) return '0 (no quota)'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

function formatBytesSize(bytes) {
  if (bytes === 0) return '0 bytes'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

function formatDuration(seconds) {
  if (seconds === 0) return '0 seconds'
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} hour${seconds / 3600 !== 1 ? 's' : ''}`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} minute${seconds / 60 !== 1 ? 's' : ''}`
  return `${seconds} second${seconds !== 1 ? 's' : ''}`
}

function decomposeBytesToUnit(bytes) {
  if (bytes === 0) return { value: '0', unit: 'MB' }
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return { value: String(gb), unit: 'GB' }
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return { value: String(mb), unit: 'MB' }
  return { value: String(bytes), unit: 'bytes' }
}

function unitToBytes(input, unit) {
  const num = Number(input)
  if (isNaN(num) || num < 0) return null
  if (unit === 'GB') return num * 1024 * 1024 * 1024
  if (unit === 'MB') return num * 1024 * 1024
  return num
}

export default function SystemSettings() {
  useEffect(() => { document.title = 'Town OS - Settings' }, [])
  const [refreshKey, setRefreshKey] = useState(0)

  const [settings] = usePolling(
    () => getClient().getSettings(),
    {},
    [refreshKey],
  )

  // --- Default Volume Quota ---
  const currentQuota = settings[DEFAULT_QUOTA_KEY] !== undefined
    ? Number(settings[DEFAULT_QUOTA_KEY])
    : DEFAULT_QUOTA_BYTES

  const [quotaInput, setQuotaInput] = useState('')
  const [quotaUnit, setQuotaUnit] = useState('GB')

  useEffect(() => {
    if (settings[DEFAULT_QUOTA_KEY] !== undefined) {
      const bytes = Number(settings[DEFAULT_QUOTA_KEY])
      if (bytes === 0) {
        setQuotaInput('0')
        setQuotaUnit('GB')
      } else {
        const { value, unit } = decomposeBytesToUnit(bytes)
        setQuotaInput(value)
        setQuotaUnit(unit)
      }
    } else {
      setQuotaInput('50')
      setQuotaUnit('GB')
    }
  }, [settings])

  function quotaToBytes() {
    return unitToBytes(quotaInput, quotaUnit)
  }

  async function handleSaveQuota(e) {
    e.preventDefault()
    const bytes = quotaToBytes()
    if (bytes === null) {
      toast.error('Invalid quota value')
      return
    }
    try {
      await getClient().setSetting(DEFAULT_QUOTA_KEY, String(bytes))
      toast.success('Default quota updated')
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.message)
    }
  }

  // --- Max Archive Size ---
  const currentMaxArchiveSize = settings[MAX_ARCHIVE_SIZE_KEY] !== undefined
    ? Number(settings[MAX_ARCHIVE_SIZE_KEY])
    : DEFAULT_MAX_ARCHIVE_SIZE_BYTES

  const [archiveSizeInput, setArchiveSizeInput] = useState('')
  const [archiveSizeUnit, setArchiveSizeUnit] = useState('MB')

  useEffect(() => {
    if (settings[MAX_ARCHIVE_SIZE_KEY] !== undefined) {
      const bytes = Number(settings[MAX_ARCHIVE_SIZE_KEY])
      const { value, unit } = decomposeBytesToUnit(bytes)
      setArchiveSizeInput(value)
      setArchiveSizeUnit(unit)
    } else {
      setArchiveSizeInput('20')
      setArchiveSizeUnit('MB')
    }
  }, [settings])

  async function handleSaveArchiveSize(e) {
    e.preventDefault()
    const bytes = unitToBytes(archiveSizeInput, archiveSizeUnit)
    if (bytes === null) {
      toast.error('Invalid archive size value')
      return
    }
    try {
      await getClient().setSetting(MAX_ARCHIVE_SIZE_KEY, String(bytes))
      toast.success('Max archive size updated')
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.message)
    }
  }

  // --- Archive Unpack Timeout ---
  const currentUnpackTimeout = settings[ARCHIVE_UNPACK_TIMEOUT_KEY] !== undefined
    ? Number(settings[ARCHIVE_UNPACK_TIMEOUT_KEY])
    : DEFAULT_ARCHIVE_UNPACK_TIMEOUT

  const [timeoutInput, setTimeoutInput] = useState('')
  const [timeoutUnit, setTimeoutUnit] = useState('seconds')

  useEffect(() => {
    if (settings[ARCHIVE_UNPACK_TIMEOUT_KEY] !== undefined) {
      const secs = Number(settings[ARCHIVE_UNPACK_TIMEOUT_KEY])
      if (secs >= 3600 && secs % 3600 === 0) {
        setTimeoutInput(String(secs / 3600))
        setTimeoutUnit('hours')
      } else if (secs >= 60 && secs % 60 === 0) {
        setTimeoutInput(String(secs / 60))
        setTimeoutUnit('minutes')
      } else {
        setTimeoutInput(String(secs))
        setTimeoutUnit('seconds')
      }
    } else {
      setTimeoutInput('120')
      setTimeoutUnit('seconds')
    }
  }, [settings])

  function timeoutToSeconds() {
    const num = Number(timeoutInput)
    if (isNaN(num) || num < 0) return null
    if (timeoutUnit === 'hours') return num * 3600
    if (timeoutUnit === 'minutes') return num * 60
    return num
  }

  async function handleSaveTimeout(e) {
    e.preventDefault()
    const secs = timeoutToSeconds()
    if (secs === null) {
      toast.error('Invalid timeout value')
      return
    }
    try {
      await getClient().setSetting(ARCHIVE_UNPACK_TIMEOUT_KEY, String(secs))
      toast.success('Archive unpack timeout updated')
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.message)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">System Settings</h1>
        <p className="text-muted-foreground">
          System-wide defaults for all users
        </p>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">Default Volume Quota</h2>
          <p className="text-sm text-muted-foreground mt-1">
            The default quota applied to new storage volumes. Set to 0 for no quota limit.
            Current value: <strong>{formatBytes(currentQuota)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveQuota} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="quota-value">Quota</Label>
            <Input
              id="quota-value"
              type="number"
              min="0"
              value={quotaInput}
              onChange={(e) => setQuotaInput(e.target.value)}
              className="w-32"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="quota-unit">Unit</Label>
            <select
              id="quota-unit"
              value={quotaUnit}
              onChange={(e) => setQuotaUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="GB">GB</option>
              <option value="MB">MB</option>
              <option value="bytes">bytes</option>
            </select>
          </div>
          <Button type="submit">Save</Button>
        </form>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">Max Archive Size</h2>
          <p className="text-sm text-muted-foreground mt-1">
            The maximum file size allowed for archive uploads.
            Current value: <strong>{formatBytesSize(currentMaxArchiveSize)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveArchiveSize} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="archive-size-value">Size</Label>
            <Input
              id="archive-size-value"
              type="number"
              min="0"
              value={archiveSizeInput}
              onChange={(e) => setArchiveSizeInput(e.target.value)}
              className="w-32"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="archive-size-unit">Unit</Label>
            <select
              id="archive-size-unit"
              value={archiveSizeUnit}
              onChange={(e) => setArchiveSizeUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="GB">GB</option>
              <option value="MB">MB</option>
              <option value="bytes">bytes</option>
            </select>
          </div>
          <Button type="submit">Save</Button>
        </form>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">Archive Unpack Timeout</h2>
          <p className="text-sm text-muted-foreground mt-1">
            The maximum time allowed for unpacking an uploaded archive before the operation is cancelled.
            Current value: <strong>{formatDuration(currentUnpackTimeout)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveTimeout} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="timeout-value">Timeout</Label>
            <Input
              id="timeout-value"
              type="number"
              min="0"
              value={timeoutInput}
              onChange={(e) => setTimeoutInput(e.target.value)}
              className="w-32"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="timeout-unit">Unit</Label>
            <select
              id="timeout-unit"
              value={timeoutUnit}
              onChange={(e) => setTimeoutUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="seconds">seconds</option>
              <option value="minutes">minutes</option>
              <option value="hours">hours</option>
            </select>
          </div>
          <Button type="submit">Save</Button>
        </form>
      </div>
    </div>
  )
}
