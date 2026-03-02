import { useState, useEffect, useCallback, useRef } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import { formatBytes } from '@/lib/utils.js'
import getClient from '@/lib/client-instance.js'
import { Link } from 'react-router-dom'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
} from 'lucide-react'

function StatCard({ to, icon: Icon, label, value, description }) {
  return (
    <Link to={to}>
      <Card className="hover:bg-accent/50 transition-colors cursor-pointer">
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-sm font-medium">{label}</CardTitle>
          <Icon className="h-4 w-4 text-muted-foreground" />
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
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      })
    }
  }, [text])
  return (
    <Button variant="ghost" size="sm" className="h-5 w-5 p-0 ml-1" onClick={handleCopy} aria-label={copied ? t('dashboard.copied_label') : t('dashboard.copy_btn_label')}>
      {copied ? <Check className="h-3 w-3 text-green-600" /> : <Copy className="h-3 w-3" />}
    </Button>
  )
}

function UpgradeBanner({ count, onDismiss, dismissing, t }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-950 p-4">
      <div className="flex items-center gap-3">
        <ArrowUpCircle className="h-5 w-5 text-blue-600 dark:text-blue-400" />
        <div>
          <p className="text-sm font-medium text-blue-900 dark:text-blue-100">
            {count} package upgrade{count !== 1 ? 's' : ''} available
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
            {ping.units.failed} failed service{ping.units.failed !== 1 ? 's' : ''}
          </Badge>
        </div>
      )}

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
              ? `${formatBytes(ping.disk_usage.used)} / ${formatBytes(ping.disk_usage.total)} used`
              : ping && (ping.installed_volumes || ping.uninstalled_volumes)
                ? `${ping.installed_volumes || 0} installed, ${ping.uninstalled_volumes || 0} uninstalled volumes`
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
              ? `${ping.recent_errors} error${ping.recent_errors !== 1 ? 's' : ''}`
              : t('dashboard.stat_no_errors')
          }
          description={t('dashboard.stat_last_5_minutes')}
        />
      </div>
    </div>
  )
}
