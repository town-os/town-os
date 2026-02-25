import { useState, useEffect, useCallback, useRef } from 'react'
import { usePolling } from '@/lib/hooks.js'
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

function CopyButton({ text }) {
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
    <Button variant="ghost" size="sm" className="h-5 w-5 p-0 ml-1" onClick={handleCopy}>
      {copied ? <Check className="h-3 w-3 text-green-600" /> : <Copy className="h-3 w-3" />}
    </Button>
  )
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const val = bytes / Math.pow(1024, i)
  return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
}

function UpgradeBanner({ count, onDismiss, dismissing }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-950 p-4">
      <div className="flex items-center gap-3">
        <ArrowUpCircle className="h-5 w-5 text-blue-600 dark:text-blue-400" />
        <div>
          <p className="text-sm font-medium text-blue-900 dark:text-blue-100">
            {count} package upgrade{count !== 1 ? 's' : ''} available
          </p>
          <Link to="/dashboard/packages" className="text-xs text-blue-700 dark:text-blue-300 underline">
            View details
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
  useEffect(() => { document.title = 'Town OS - Dashboard' }, [])
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
        <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-muted-foreground">
          System overview and quick navigation
        </p>
        {(ping?.external_ip || ping?.internal_ip) && (
          <div className="flex items-center gap-4 mt-1 text-sm text-muted-foreground">
            {ping.external_ip && (
              <span className="flex items-center">
                External IP: <span className="font-mono ml-1">{ping.external_ip}</span>
                <CopyButton text={ping.external_ip} />
              </span>
            )}
            {ping.internal_ip && (
              <span className="flex items-center">
                Internal IP: <span className="font-mono ml-1">{ping.internal_ip}</span>
                <CopyButton text={ping.internal_ip} />
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
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <StatCard
          to="/dashboard/storage"
          icon={HardDrive}
          label="Filesystems"
          value={ping?.filesystems}
          description={
            ping?.disk_usage
              ? `${formatBytes(ping.disk_usage.used)} / ${formatBytes(ping.disk_usage.total)} used`
              : ping && (ping.installed_volumes || ping.uninstalled_volumes)
                ? `${ping.installed_volumes || 0} installed, ${ping.uninstalled_volumes || 0} uninstalled volumes`
                : 'Btrfs subvolumes'
          }
        />
        <StatCard
          to="/dashboard/users"
          icon={Users}
          label="Accounts"
          value={ping?.accounts}
          description="User accounts"
        />
        <StatCard
          to="/dashboard/system"
          icon={Cog}
          label="Services"
          value={
            ping?.units
              ? `${ping.units.active} / ${ping.units.total}`
              : undefined
          }
          description="Active systemd units"
        />
        <StatCard
          to="/dashboard/packages"
          icon={Package}
          label="Packages"
          value={ping?.packages}
          description={
            ping ? `${ping.installed} installed` : undefined
          }
        />
        <StatCard
          to="/dashboard/packages"
          icon={FolderGit2}
          label="Repositories"
          value={ping?.repositories}
          description="Package repositories"
        />
        <StatCard
          to="/dashboard/log"
          icon={FileText}
          label="Audit Log"
          value={
            ping?.recent_errors > 0
              ? `${ping.recent_errors} error${ping.recent_errors !== 1 ? 's' : ''}`
              : 'No errors'
          }
          description="Last 5 minutes"
        />
      </div>
    </div>
  )
}
