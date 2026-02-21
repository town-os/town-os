import { Link, useLocation } from 'react-router-dom'
import { useRequireAuth } from '@/lib/hooks.js'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import {
  LayoutDashboard,
  HardDrive,
  Users,
  Cog,
  Package,
  FileText,
  LogOut,
  Activity,
  Settings,
} from 'lucide-react'

const NAV_ITEMS = [
  { to: '/dashboard', label: 'Home', icon: LayoutDashboard },
  { to: '/dashboard/storage', label: 'Storage', icon: HardDrive },
  { to: '/dashboard/users', label: 'Users', icon: Users },
  { to: '/dashboard/system', label: 'Services', icon: Cog },
  { to: '/dashboard/packages', label: 'Packages', icon: Package },
  { to: '/dashboard/log', label: 'Audit Log', icon: FileText },
  { to: '/dashboard/settings', label: 'Settings', icon: Settings, adminOnly: true },
]

export default function Dashboard({ children }) {
  const account = useRequireAuth()
  const location = useLocation()

  const [ping, , loading] = usePolling(
    () => getClient().ping().catch(() => ({ status: 'error' })),
    null,
    [],
    10000,
  )

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-50 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
        <div className="flex h-14 items-center px-6 gap-4">
          <Link to="/dashboard" className="font-bold text-lg mr-4">
            Town OS
          </Link>
          <Separator orientation="vertical" className="h-6" />
          <nav className="flex items-center gap-1">
            {NAV_ITEMS.filter(
              (item) => !item.adminOnly || account?.admin,
            ).map(({ to, label, icon: Icon }) => {
              const active = location.pathname === to
              return (
                <Button
                  key={to}
                  variant={active ? 'secondary' : 'ghost'}
                  size="sm"
                  asChild
                >
                  <Link to={to}>
                    <Icon className="h-4 w-4 mr-1" />
                    {label}
                  </Link>
                </Button>
              )
            })}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            {loading && !ping && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground animate-pulse">
                <Activity className="h-3 w-3" />
                <Badge variant="outline" className="text-xs">
                  Loading...
                </Badge>
              </div>
            )}
            {ping && ping.status !== 'ok' && (
              <div className="flex items-center gap-2 text-sm">
                <Activity className="h-3 w-3 text-red-600" />
                <Badge className="bg-red-600 text-white font-bold text-xs hover:bg-red-600/90">
                  API Offline
                </Badge>
              </div>
            )}
            {ping && ping.status === 'ok' && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Activity className="h-3 w-3" />
                <Badge variant="outline" className="text-xs">
                  Online
                </Badge>
              </div>
            )}
            {account && (
              <span className="text-sm text-muted-foreground">
                {account.username}
                {account.admin && (
                  <Badge variant="secondary" className="ml-1 text-xs">
                    admin
                  </Badge>
                )}
              </span>
            )}
            <Button variant="ghost" size="sm" asChild>
              <Link to="/logout">
                <LogOut className="h-4 w-4 mr-1" />
                Logout
              </Link>
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-6">{children}</main>
    </div>
  )
}
