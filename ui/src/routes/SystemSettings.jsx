import { useState, useEffect, useRef } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { toast } from 'sonner'

const DEFAULT_QUOTA_KEY = 'default_quota'
const DEFAULT_QUOTA_BYTES = 50 * 1024 * 1024 * 1024 // 50 GB
const MAX_ARCHIVE_SIZE_KEY = 'max_archive_size'
const DEFAULT_MAX_ARCHIVE_SIZE_BYTES = 1024 * 1024 * 1024 // 1 GB
const ARCHIVE_UNPACK_TIMEOUT_KEY = 'archive_unpack_timeout'
const DEFAULT_ARCHIVE_UNPACK_TIMEOUT = 600 // seconds (10 min)
const PEER_TTL_KEY = 'peer_ttl'
const DEFAULT_PEER_TTL = 7200 // seconds (2 hours)
const PROTON_IMAGE_KEY = 'proton_image'
const MONITORING_BACKEND_KEY = 'monitoring_backend'
const DNS_RESOLUTION_MODE_KEY = 'dns_resolution_mode'
const DNS_LOCAL_FORWARDERS_KEY = 'dns_local_forwarders'

function formatBytes(t, bytes) {
  if (bytes === 0) return t('settings.format_no_quota')
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

function formatBytesSize(t, bytes) {
  if (bytes === 0) return t('settings.format_0_bytes')
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1 && gb === Math.floor(gb)) return `${gb} GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1 && mb === Math.floor(mb)) return `${mb} MB`
  return `${bytes} bytes`
}

function formatDuration(t, seconds) {
  if (seconds === 0) return t('settings.format_0_seconds')
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
  const { t, setLocale } = useI18n()
  useEffect(() => { document.title = t('settings.page_title') }, [t])
  const [refreshKey, setRefreshKey] = useState(0)

  const [settings] = usePolling(
    () => getClient().getSettings(),
    {},
    [refreshKey],
  )

  // Backend build feature flag: true when the systemcontroller was built
  // with `-tags proton`. When false the Proton runner image card is hidden
  // and the `proton_image` setting is not seeded into the DB.
  const [ping] = usePolling(
    () => getClient().ping().catch(() => ({})),
    {},
    [],
    60000,
  )
  const protonEnabled = !!ping?.proton_enabled

  // --- Language ---
  const [localeData, setLocaleData] = useState(null)
  const [showExtended, setShowExtended] = useState(false)
  const [selectedLocale, setSelectedLocale] = useState('')

  useEffect(() => {
    getClient().getLocales().then((data) => {
      setLocaleData(data)
      setSelectedLocale(data.current)
    }).catch(() => {})
  }, [refreshKey])

  async function handleSaveLanguage(e) {
    e.preventDefault()
    if (!selectedLocale) return
    if (selectedLocale === localeData?.current) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    try {
      await getClient().setSetting('locale', selectedLocale)
      setLocale(selectedLocale)
      toast.success(t('settings.toast_language_updated'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

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
      toast.error(t('settings.error_invalid_quota'))
      return
    }
    if (bytes === currentQuota) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    try {
      await getClient().setSetting(DEFAULT_QUOTA_KEY, String(bytes))
      toast.success(t('settings.toast_quota_updated'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
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
      toast.error(t('settings.error_invalid_archive_size'))
      return
    }
    if (bytes === currentMaxArchiveSize) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    try {
      await getClient().setSetting(MAX_ARCHIVE_SIZE_KEY, String(bytes))
      toast.success(t('settings.toast_archive_size_updated'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
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
      toast.error(t('settings.error_invalid_timeout'))
      return
    }
    if (secs === currentUnpackTimeout) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    try {
      await getClient().setSetting(ARCHIVE_UNPACK_TIMEOUT_KEY, String(secs))
      toast.success(t('settings.toast_timeout_updated'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  // --- WireGuard Peer TTL ---
  // How long a WireGuard-only enrollment stays valid before the reaper removes
  // it. A long-lived client (the portal) refreshes its peer before this elapses.
  const currentPeerTTL = settings[PEER_TTL_KEY] !== undefined
    ? Number(settings[PEER_TTL_KEY])
    : DEFAULT_PEER_TTL

  const [peerTTLInput, setPeerTTLInput] = useState('')
  const [peerTTLUnit, setPeerTTLUnit] = useState('hours')

  useEffect(() => {
    if (settings[PEER_TTL_KEY] !== undefined) {
      const secs = Number(settings[PEER_TTL_KEY])
      if (secs >= 3600 && secs % 3600 === 0) {
        setPeerTTLInput(String(secs / 3600))
        setPeerTTLUnit('hours')
      } else if (secs >= 60 && secs % 60 === 0) {
        setPeerTTLInput(String(secs / 60))
        setPeerTTLUnit('minutes')
      } else {
        setPeerTTLInput(String(secs))
        setPeerTTLUnit('seconds')
      }
    } else {
      setPeerTTLInput('2')
      setPeerTTLUnit('hours')
    }
  }, [settings])

  function peerTTLToSeconds() {
    const num = Number(peerTTLInput)
    if (isNaN(num) || num <= 0) return null
    if (peerTTLUnit === 'hours') return num * 3600
    if (peerTTLUnit === 'minutes') return num * 60
    return num
  }

  async function handleSavePeerTTL(e) {
    e.preventDefault()
    const secs = peerTTLToSeconds()
    if (secs === null) {
      toast.error(t('settings.error_invalid_peer_ttl'))
      return
    }
    if (secs === currentPeerTTL) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    try {
      await getClient().setSetting(PEER_TTL_KEY, String(secs))
      toast.success(t('settings.toast_peer_ttl_updated'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  // --- Proton Runner Image ---
  const currentProtonImage = settings[PROTON_IMAGE_KEY] || ''

  const [protonImageInput, setProtonImageInput] = useState('')

  useEffect(() => {
    setProtonImageInput(settings[PROTON_IMAGE_KEY] || '')
  }, [settings])

  async function handleSaveProtonImage(e) {
    e.preventDefault()
    if (protonImageInput === currentProtonImage) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    try {
      await getClient().setSetting(PROTON_IMAGE_KEY, protonImageInput)
      toast.success(t('settings.toast_proton_image_updated'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  // --- Monitoring Backend ---
  const currentMonitoringBackend = settings[MONITORING_BACKEND_KEY] || 'uplot'

  const [monitoringBackendInput, setMonitoringBackendInput] = useState('uplot')
  const [monitoringBackendSaving, setMonitoringBackendSaving] = useState(false)
  const monitoringToastId = useRef('monitoring-backend')

  useEffect(() => {
    setMonitoringBackendInput(settings[MONITORING_BACKEND_KEY] || 'uplot')
  }, [settings])

  // pollMonitoringReady polls /monitoring/status until the backend matches
  // the requested value AND the monitoring-ui unit is active, or a timeout
  // elapses. Returns true on success, false on timeout or persistent error.
  async function pollMonitoringReady(wantBackend, { timeoutMs = 30000, intervalMs = 1500 } = {}) {
    const startTime = Date.now()
    while (Date.now() - startTime < timeoutMs) {
      try {
        const status = await getClient().monitoringStatus()
        if (status && status.backend === wantBackend && status.monitoring_ui) {
          return true
        }
      } catch {
        // Transient errors while systemd is restarting the unit are
        // expected; keep polling until the timeout.
      }
      await new Promise((resolve) => setTimeout(resolve, intervalMs))
    }
    return false
  }

  async function handleSaveMonitoringBackend(e) {
    e.preventDefault()
    const wantBackend = monitoringBackendInput
    if (wantBackend === currentMonitoringBackend) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    setMonitoringBackendSaving(true)
    toast.loading(t('settings.toast_monitoring_restarting'), { id: monitoringToastId.current })
    try {
      await getClient().setSetting(MONITORING_BACKEND_KEY, wantBackend)
      const ready = await pollMonitoringReady(wantBackend)
      toast.dismiss(monitoringToastId.current)
      if (ready) {
        toast.success(t('settings.toast_monitoring_ready', {
          backend: wantBackend === 'grafana' ? 'Grafana' : 'uPlot',
        }))
      } else {
        toast.warning(t('settings.toast_monitoring_timeout'))
      }
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.dismiss(monitoringToastId.current)
      toast.error(err.detail || err.message)
    } finally {
      setMonitoringBackendSaving(false)
    }
  }

  // --- DNS Resolution Mode ---
  // Auto tries the root servers, then DoH/DoT, then the forwarders, then a
  // public resolver. Recursive is roots-only with no fallback; forward goes
  // straight to the upstream resolvers.
  const currentResolutionMode = settings[DNS_RESOLUTION_MODE_KEY] || 'auto'

  const [resolutionModeInput, setResolutionModeInput] = useState('auto')
  const [resolutionModeSaving, setResolutionModeSaving] = useState(false)

  useEffect(() => {
    setResolutionModeInput(settings[DNS_RESOLUTION_MODE_KEY] || 'auto')
  }, [settings])

  async function handleSaveResolutionMode(e) {
    e.preventDefault()
    if (resolutionModeInput === currentResolutionMode) {
      toast.info(t('settings.toast_nothing_to_do'))
      return
    }
    setResolutionModeSaving(true)
    try {
      await getClient().setSetting(DNS_RESOLUTION_MODE_KEY, resolutionModeInput)
      toast.success(t('settings.toast_dns_resolution_saved'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setResolutionModeSaving(false)
    }
  }

  // --- Local DNS Forwarders ---
  // Repoints rolodex's forwarder list at the resolvers this box's own network
  // handed it, for a network that blocks external DNS. The effective list comes
  // from /dns/status rather than being inferred from the setting: discovery can
  // find nothing usable, in which case the switch reads as on and the public
  // defaults are still what rolodex holds.
  const localForwardersOn = String(settings[DNS_LOCAL_FORWARDERS_KEY]) === 'true'
  const [localForwardersSaving, setLocalForwardersSaving] = useState(false)

  const [dnsStatus] = usePolling(
    () => getClient().dnsStatus().catch(() => ({})),
    {},
    [refreshKey],
  )
  const effectiveForwarders = dnsStatus?.forwarders || []

  async function handleToggleLocalForwarders(next) {
    setLocalForwardersSaving(true)
    try {
      await getClient().setSetting(DNS_LOCAL_FORWARDERS_KEY, String(next))
      toast.success(t('settings.toast_dns_local_forwarders_saved'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setLocalForwardersSaving(false)
    }
  }

  const populated = new Set(localeData?.populated || [])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('settings.title')}</h1>
        <p className="text-muted-foreground">
          {t('settings.description')}
        </p>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.language_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.language_description')}
          </p>
        </div>

        {localeData && (
          <form onSubmit={handleSaveLanguage} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="language-select">{t('settings.language_label')}</Label>
              <select
                id="language-select"
                value={selectedLocale}
                onChange={(e) => setSelectedLocale(e.target.value)}
                className="flex h-9 w-full max-w-xs rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
              >
                {localeData.common_languages.map((lang) => (
                  <option key={lang.code} value={lang.code} disabled={!populated.has(lang.code)}>
                    {lang.native_name} ({lang.english_name}){!populated.has(lang.code) ? ' *' : ''}
                  </option>
                ))}
                {showExtended && localeData.extended_locales.map((lang) => (
                  <option key={lang.code} value={lang.code} disabled={!populated.has(lang.code)}>
                    {lang.code} - {lang.native_name} ({lang.english_name}){!populated.has(lang.code) ? ' *' : ''}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex items-center gap-3">
              <Button type="submit">{t('settings.save_btn')}</Button>
              <button
                type="button"
                className="text-sm text-muted-foreground underline hover:text-foreground"
                onClick={() => setShowExtended(!showExtended)}
              >
                {showExtended ? t('settings.hide_all_locales') : t('settings.show_all_locales')}
              </button>
            </div>
          </form>
        )}
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.quota_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.quota_description')}{' '}
            {t('settings.current_value', { value: '' })}<strong>{formatBytes(t, currentQuota)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveQuota} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="quota-value">{t('settings.quota_label')}</Label>
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
            <Label htmlFor="quota-unit">{t('settings.unit_label')}</Label>
            <select
              id="quota-unit"
              value={quotaUnit}
              onChange={(e) => setQuotaUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="GB">{t('settings.unit_option_gb')}</option>
              <option value="MB">{t('settings.unit_option_mb')}</option>
              <option value="bytes">{t('settings.unit_option_bytes')}</option>
            </select>
          </div>
          <Button type="submit">{t('settings.save_btn')}</Button>
        </form>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.archive_size_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.archive_size_description')}{' '}
            {t('settings.current_value', { value: '' })}<strong>{formatBytesSize(t, currentMaxArchiveSize)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveArchiveSize} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="archive-size-value">{t('settings.archive_size_label')}</Label>
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
            <Label htmlFor="archive-size-unit">{t('settings.unit_label')}</Label>
            <select
              id="archive-size-unit"
              value={archiveSizeUnit}
              onChange={(e) => setArchiveSizeUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="GB">{t('settings.unit_option_gb')}</option>
              <option value="MB">{t('settings.unit_option_mb')}</option>
              <option value="bytes">{t('settings.unit_option_bytes')}</option>
            </select>
          </div>
          <Button type="submit">{t('settings.save_btn')}</Button>
        </form>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.timeout_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.timeout_description')}{' '}
            {t('settings.current_value', { value: '' })}<strong>{formatDuration(t, currentUnpackTimeout)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveTimeout} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="timeout-value">{t('settings.timeout_label')}</Label>
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
            <Label htmlFor="timeout-unit">{t('settings.unit_label')}</Label>
            <select
              id="timeout-unit"
              value={timeoutUnit}
              onChange={(e) => setTimeoutUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="seconds">{t('settings.unit_option_seconds')}</option>
              <option value="minutes">{t('settings.unit_option_minutes')}</option>
              <option value="hours">{t('settings.unit_option_hours')}</option>
            </select>
          </div>
          <Button type="submit">{t('settings.save_btn')}</Button>
        </form>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.peer_ttl_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.peer_ttl_description')}{' '}
            {t('settings.current_value', { value: '' })}<strong>{formatDuration(t, currentPeerTTL)}</strong>
          </p>
        </div>

        <form onSubmit={handleSavePeerTTL} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="peer-ttl-value">{t('settings.peer_ttl_label')}</Label>
            <Input
              id="peer-ttl-value"
              type="number"
              min="1"
              value={peerTTLInput}
              onChange={(e) => setPeerTTLInput(e.target.value)}
              className="w-32"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="peer-ttl-unit">{t('settings.unit_label')}</Label>
            <select
              id="peer-ttl-unit"
              value={peerTTLUnit}
              onChange={(e) => setPeerTTLUnit(e.target.value)}
              className="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs"
            >
              <option value="seconds">{t('settings.unit_option_seconds')}</option>
              <option value="minutes">{t('settings.unit_option_minutes')}</option>
              <option value="hours">{t('settings.unit_option_hours')}</option>
            </select>
          </div>
          <Button type="submit">{t('settings.save_btn')}</Button>
        </form>
      </div>

      {protonEnabled && (
        <div className="rounded-lg border p-6 space-y-6">
          <div>
            <h2 className="text-lg font-semibold">{t('settings.proton_image_title')}</h2>
            <p className="text-sm text-muted-foreground mt-1">
              {t('settings.proton_image_description')}{' '}
              {t('settings.current_value', { value: '' })}<strong>{currentProtonImage || t('settings.proton_image_current_not_set')}</strong>
            </p>
          </div>

          <form onSubmit={handleSaveProtonImage} className="flex items-end gap-3">
            <div className="space-y-2">
              <Label htmlFor="proton-image-value">{t('settings.proton_image_label')}</Label>
              <Input
                id="proton-image-value"
                type="text"
                value={protonImageInput}
                onChange={(e) => setProtonImageInput(e.target.value)}
                placeholder={t('settings.proton_image_placeholder')}
                className="w-96"
              />
            </div>
            <Button type="submit">{t('settings.save_btn')}</Button>
          </form>
        </div>
      )}

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.dns_resolution_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.dns_resolution_description')}{' '}
            {t('settings.current_value', { value: '' })}
            <strong>{t(`settings.dns_resolution_option_${currentResolutionMode}`)}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveResolutionMode} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="dns-resolution-mode-select">{t('settings.dns_resolution_label')}</Label>
            <select
              id="dns-resolution-mode-select"
              value={resolutionModeInput}
              onChange={(e) => setResolutionModeInput(e.target.value)}
              disabled={resolutionModeSaving}
              className="flex h-9 w-full max-w-xs rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="auto">{t('settings.dns_resolution_option_auto')}</option>
              <option value="recursive">{t('settings.dns_resolution_option_recursive')}</option>
              <option value="forward">{t('settings.dns_resolution_option_forward')}</option>
            </select>
          </div>
          <Button type="submit" disabled={resolutionModeSaving}>
            {resolutionModeSaving ? t('settings.saving_btn') : t('settings.save_btn')}
          </Button>
        </form>
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold">{t('settings.dns_local_forwarders_title')}</h2>
            <p className="text-sm text-muted-foreground mt-1">
              {t('settings.dns_local_forwarders_description')}{' '}
              {t('settings.current_value', { value: '' })}
              <strong>
                {localForwardersOn
                  ? t('settings.dns_local_forwarders_on')
                  : t('settings.dns_local_forwarders_off')}
              </strong>
            </p>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <Label htmlFor="dns-local-forwarders-switch">
              {t('settings.dns_local_forwarders_label')}
            </Label>
            <Switch
              id="dns-local-forwarders-switch"
              checked={localForwardersOn}
              disabled={localForwardersSaving}
              onCheckedChange={handleToggleLocalForwarders}
            />
          </div>
        </div>

        {effectiveForwarders.length > 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('settings.dns_local_forwarders_active', { value: '' })}
            <code className="font-mono">{effectiveForwarders.join(', ')}</code>
          </p>
        ) : (
          localForwardersOn && (
            <p className="text-sm text-muted-foreground">
              {t('settings.dns_local_forwarders_none')}
            </p>
          )
        )}
      </div>

      <div className="rounded-lg border p-6 space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{t('settings.monitoring_title')}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t('settings.monitoring_description')}{' '}
            {t('settings.current_value', { value: '' })}<strong>{currentMonitoringBackend === 'grafana' ? 'Grafana' : 'uPlot'}</strong>
          </p>
        </div>

        <form onSubmit={handleSaveMonitoringBackend} className="flex items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="monitoring-backend-select">{t('settings.monitoring_label')}</Label>
            <select
              id="monitoring-backend-select"
              value={monitoringBackendInput}
              onChange={(e) => setMonitoringBackendInput(e.target.value)}
              disabled={monitoringBackendSaving}
              className="flex h-9 w-full max-w-xs rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="uplot">{t('settings.monitoring_option_uplot')}</option>
              <option value="grafana">{t('settings.monitoring_option_grafana')}</option>
            </select>
          </div>
          <Button type="submit" disabled={monitoringBackendSaving}>
            {monitoringBackendSaving ? t('settings.saving_btn') : t('settings.save_btn')}
          </Button>
        </form>
      </div>

    </div>
  )
}
