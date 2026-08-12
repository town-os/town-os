import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import { formatBytes } from '@/lib/utils.js'
import getClient from '@/lib/client-instance.js'
import { Link } from 'react-router-dom'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  HardDrive,
  Users,
  Package,
  Cog,
  FolderGit2,
  FileText,
  Copy,
  Check,
  ArrowUpCircle,
  X,
  CircleCheck,
  CircleX,
  Circle,
  ExternalLink,
} from 'lucide-react'

function StatCard({ to, icon, label, value, description }) {
  const IconComponent = icon
  return (
    <Link to={to}>
      <Card className="hover:bg-accent/50 transition-colors cursor-pointer">
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-sm font-medium">{label}</CardTitle>
          <IconComponent className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{value ?? '...'}</div>
          {description && (
            <p className="text-xs text-muted-foreground">{description}</p>
          )}
        </CardContent>
      </Card>
    </Link>
  )
}

function CopyButton({ text, t }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = useCallback(() => {
    function onSuccess() {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }

    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(onSuccess)
      return
    }

    // Fallback for non-secure contexts (HTTP with non-localhost hostname).
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    try {
      document.execCommand('copy')
      onSuccess()
    } finally {
      document.body.removeChild(ta)
    }
  }, [text])
  return (
    <Button variant="ghost" size="sm" className="h-5 w-5 p-0 ml-1" onClick={handleCopy} aria-label={copied ? t('dashboard.copied_label') : t('dashboard.copy_btn_label')}>
      {copied ? <Check className="h-3 w-3 text-green-600" /> : <Copy className="h-3 w-3" />}
    </Button>
  )
}

// parsePackageIdentifier splits an identifier of the form
// "<repo>/<name>@<version>" into its components. The name segment is the
// portion between the first slash and the final @, so it correctly
// handles pretty dependency identifiers like "core/gitea/postgres@1.0"
// (returning name = "gitea/postgres"). When called on a raw
// package_identifier the name still carries the flat --dep-- form, which
// is what /packages/installed/info expects as its name argument.
function parsePackageIdentifier(id) {
  if (!id) return null
  const atIdx = id.lastIndexOf('@')
  if (atIdx === -1) return null
  const left = id.slice(0, atIdx)
  const version = id.slice(atIdx + 1)
  const slashIdx = left.indexOf('/')
  if (slashIdx === -1) return null
  return { repo: left.slice(0, slashIdx), name: left.slice(slashIdx + 1), version }
}

function StatusIcon({ state }) {
  if (state === 'active') return <CircleCheck className="h-4 w-4 text-green-600 shrink-0" />
  if (state === 'failed') return <CircleX className="h-4 w-4 text-red-600 shrink-0" />
  return <Circle className="h-4 w-4 text-muted-foreground shrink-0" />
}

function httpsNotes(info) {
  if (!info || !info.notes) return []
  const out = []
  for (const [label, value] of Object.entries(info.notes)) {
    if (info.note_types?.[label] !== 'url') continue
    if (typeof value !== 'string' || !value.startsWith('https://')) continue
    out.push({ label, value })
  }
  return out
}

// serviceLink builds the deep link to a service's row on the services
// screen. The search term is the root's package_identifier, which the
// units-tree endpoint matches against the node's own fields — so the
// screen opens showing that one service (and its deps) rather than the
// whole list for the operator to hunt through.
function serviceLink(packageIdentifier) {
  return `/dashboard/system?search=${encodeURIComponent(packageIdentifier)}`
}

// ServicesPanel renders a flat table of services that expose at least
// one HTTPS URL note — status icon on the left, package name in the
// middle, clickable URLs on the right. Services without links (and
// dependency sub-packages, which are internal) are filtered out
// entirely so the dashboard stays focused on user-reachable entry
// points. Both the status icon and the name link to that service's own
// row on /dashboard/system — the package is only interesting here as a
// running service, and the packages screen answers a different question
// (what is installed / upgradable) than the one a dashboard click asks.
function ServicesPanel({ roots, notesMap, t }) {
  const rows = (roots || [])
    .map((node) => ({ node, links: httpsNotes(notesMap[node.package_identifier]) }))
    .filter(({ links }) => links.length > 0)

  if (rows.length === 0) return null

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-medium">{t('dashboard.services_title')}</CardTitle>
      </CardHeader>
      <CardContent className="pt-0">
        <Table>
          <TableBody>
            {rows.map(({ node, links }) => {
              const displayId = node.display_identifier || node.package_identifier
              const parsed = parsePackageIdentifier(displayId)
              const displayName = parsed ? parsed.name : displayId
              const to = serviceLink(node.package_identifier)
              return (
                <TableRow key={node.package_identifier}>
                  <TableCell className="w-8 py-2">
                    <Link
                      to={to}
                      aria-label={t('dashboard.services_status_label', { name: displayName, state: node.ActiveState })}
                    >
                      <StatusIcon state={node.ActiveState} />
                    </Link>
                  </TableCell>
                  <TableCell className="py-2">
                    <Link
                      to={to}
                      className="font-mono text-sm font-medium underline-offset-2 hover:underline"
                    >
                      {displayName}
                    </Link>
                  </TableCell>
                  <TableCell className="py-2 text-right">
                    <div className="flex flex-col items-end gap-0.5">
                      {links.map(({ label, value }) => (
                        <a
                          key={label}
                          href={value}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-1 text-xs text-primary underline underline-offset-2"
                        >
                          <ExternalLink className="h-3 w-3 shrink-0" />
                          <span className="text-muted-foreground">{label}:</span>
                          <span className="font-mono">{value}</span>
                        </a>
                      ))}
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function UpgradeBanner({ count, onDismiss, dismissing, t }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-950 p-4">
      <div className="flex items-center gap-3">
        <ArrowUpCircle className="h-5 w-5 text-blue-600 dark:text-blue-400" />
        <div>
          <p className="text-sm font-medium text-blue-900 dark:text-blue-100">
            {t('dashboard.upgrade_available', { count, s: count !== 1 ? 's' : '' })}
          </p>
          <Link to="/dashboard/packages" className="text-xs text-blue-700 dark:text-blue-300 underline">
            {t('dashboard.upgrade_view_details')}
          </Link>
        </div>
      </div>
      <Button
        variant="ghost"
        size="sm"
        className="h-8 w-8 p-0"
        onClick={onDismiss}
        disabled={dismissing}
      >
        <X className="h-4 w-4" />
      </Button>
    </div>
  )
}

export default function DashboardHome() {
  const { t } = useI18n()
  useEffect(() => { document.title = t('dashboard.page_title') }, [t])
  const [ping, , loading] = usePolling(() => getClient().ping(), null, [], 60000)
  const [unitData] = usePolling(
    () => getClient().listUnitsTree('package_identifier', 'asc', undefined, undefined, undefined),
    { entries: [] },
    [],
    60000,
  )
  // The tree endpoint already nests dependency services under their parent;
  // roots are the top-level packages. No client-side filter needed.
  const roots = useMemo(() => unitData.entries || [], [unitData.entries])
  const [notesMap, setNotesMap] = useState({})
  const notesFetchedRef = useRef(new Set())

  useEffect(() => {
    // Only root packages render HTTPS notes on the dashboard, so only
    // fetch notes for roots — deps never show links.
    for (const node of roots) {
      const id = node.package_identifier
      if (!id || notesFetchedRef.current.has(id)) continue
      notesFetchedRef.current.add(id)
      const parsed = parsePackageIdentifier(id)
      if (!parsed) continue
      getClient().getInstalledInfo(parsed.repo, parsed.name, parsed.version)
        .then((info) => {
          if (info.notes && Object.keys(info.notes).length > 0) {
            setNotesMap((prev) => ({ ...prev, [id]: info }))
          }
        })
        .catch(() => {})
    }
  }, [roots])

  const [dismissing, setDismissing] = useState(false)
  const lastDismissedRef = useRef(null)

  const showUpgradeBanner = ping &&
    ping.upgrades_available > 0 &&
    !ping.upgrades_dismissed &&
    lastDismissedRef.current !== ping.upgrades_available

  const handleDismiss = useCallback(async () => {
    setDismissing(true)
    try {
      await getClient().dismissUpgrades()
      lastDismissedRef.current = ping?.upgrades_available
    } catch {
      // ignore
    } finally {
      setDismissing(false)
    }
  }, [ping?.upgrades_available])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('dashboard.title')}</h1>
        <p className="text-muted-foreground">
          {t('dashboard.description')}
        </p>
        {(ping?.external_ip || ping?.internal_ip) && (
          <div className="flex items-center gap-4 mt-1 text-sm text-muted-foreground">
            {ping.external_ip && (
              <span className="flex items-center">
                {t('dashboard.external_ip')} <span className="font-mono ml-1">{ping.external_ip}</span>
                <CopyButton text={ping.external_ip} t={t} />
              </span>
            )}
            {ping.internal_ip && (
              <span className="flex items-center">
                {t('dashboard.internal_ip')} <span className="font-mono ml-1">{ping.internal_ip}</span>
                <CopyButton text={ping.internal_ip} t={t} />
              </span>
            )}
          </div>
        )}
      </div>

      {showUpgradeBanner && (
        <UpgradeBanner
          count={ping.upgrades_available}
          onDismiss={handleDismiss}
          dismissing={dismissing}
          t={t}
        />
      )}

      {ping && ping.units && ping.units.failed > 0 && (
        <div className="flex items-center gap-2">
          <Badge variant="destructive">
            {t('dashboard.failed_services', { count: ping.units.failed, s: ping.units.failed !== 1 ? 's' : '' })}
          </Badge>
        </div>
      )}

      <ServicesPanel roots={roots} notesMap={notesMap} t={t} />

      {loading && !ping && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('dashboard.loading')}</div>
      )}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <StatCard
          to="/dashboard/storage"
          icon={HardDrive}
          label={t('dashboard.stat_filesystems')}
          value={ping?.filesystems}
          description={
            ping?.disk_usage
              ? t('dashboard.stat_disk_usage', { used: formatBytes(ping.disk_usage.used), total: formatBytes(ping.disk_usage.total) })
              : ping && (ping.installed_volumes || ping.uninstalled_volumes)
                ? t('dashboard.stat_volumes_summary', { installed: ping.installed_volumes || 0, uninstalled: ping.uninstalled_volumes || 0 })
                : t('dashboard.stat_btrfs_subvolumes')
          }
        />
        <StatCard
          to="/dashboard/users"
          icon={Users}
          label={t('dashboard.stat_accounts')}
          value={ping?.accounts}
          description={t('dashboard.stat_user_accounts')}
        />
        <StatCard
          to="/dashboard/system"
          icon={Cog}
          label={t('dashboard.stat_services')}
          value={
            ping?.units
              ? `${ping.units.active} / ${ping.units.total}`
              : undefined
          }
          description={t('dashboard.stat_active_units')}
        />
        <StatCard
          to="/dashboard/packages"
          icon={Package}
          label={t('dashboard.stat_packages')}
          value={ping?.packages}
          description={
            ping ? t('dashboard.stat_installed_count', { count: ping.installed }) : undefined
          }
        />
        <StatCard
          to="/dashboard/packages"
          icon={FolderGit2}
          label={t('dashboard.stat_repositories')}
          value={ping?.repositories}
          description={t('dashboard.stat_package_repositories')}
        />
        <StatCard
          to="/dashboard/log"
          icon={FileText}
          label={t('dashboard.stat_audit_log')}
          value={
            ping?.recent_errors > 0
              ? t('dashboard.stat_errors', { count: ping.recent_errors, s: ping.recent_errors !== 1 ? 's' : '' })
              : t('dashboard.stat_no_errors')
          }
          description={t('dashboard.stat_last_5_minutes')}
        />
      </div>
    </div>
  )
}
