import { useEffect } from 'react'
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
import {
  HardDrive,
  Users,
  Package,
  Cog,
  FolderGit2,
  FileText,
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

export default function DashboardHome() {
  useEffect(() => { document.title = 'Town OS - Dashboard' }, [])
  const [ping] = usePolling(() => getClient().ping(), null, [], 10000)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-muted-foreground">
          System overview and quick navigation
        </p>
      </div>

      {ping && (
        <div className="flex items-center gap-2">
          <Badge variant={ping.status === 'ok' ? 'default' : 'destructive'}>
            {ping.status === 'ok' ? 'System Online' : 'System Offline'}
          </Badge>
          {ping.units && (
            <>
              <Badge variant="outline">
                {ping.units.active} active / {ping.units.total} units
              </Badge>
              {ping.units.failed > 0 && (
                <Badge variant="destructive">
                  {ping.units.failed} failed
                </Badge>
              )}
            </>
          )}
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <StatCard
          to="/dashboard/storage"
          icon={HardDrive}
          label="Filesystems"
          value={ping?.filesystems}
          description="Btrfs subvolumes"
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
