import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'

const DEFAULT_QUOTA_KEY = 'default_quota'
const DEFAULT_QUOTA_BYTES = 50 * 1024 * 1024 * 1024 // 50 GB

function formatBytes(bytes) {
  if (bytes === 0) return '0 (no quota)'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

export default function SystemSettings() {
  useEffect(() => { document.title = 'Town OS - Settings' }, [])
  const [refreshKey, setRefreshKey] = useState(0)

  const [settings] = usePolling(
    () => getClient().getSettings(),
    {},
    [refreshKey],
  )

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
        const gb = bytes / (1024 * 1024 * 1024)
        if (gb >= 1 && gb === Math.floor(gb)) {
          setQuotaInput(String(gb))
          setQuotaUnit('GB')
        } else {
          const mb = bytes / (1024 * 1024)
          if (mb >= 1 && mb === Math.floor(mb)) {
            setQuotaInput(String(mb))
            setQuotaUnit('MB')
          } else {
            setQuotaInput(String(bytes))
            setQuotaUnit('bytes')
          }
        }
      }
    } else {
      setQuotaInput('50')
      setQuotaUnit('GB')
    }
  }, [settings])

  function quotaToBytes() {
    const num = Number(quotaInput)
    if (isNaN(num) || num < 0) return null
    if (quotaUnit === 'GB') return num * 1024 * 1024 * 1024
    if (quotaUnit === 'MB') return num * 1024 * 1024
    return num
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
    </div>
  )
}
