import { Link, useLocation } from 'react-router-dom'
import { useRequireAuth } from '@/lib/hooks.js'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from '@/components/ui/tooltip'
import {
  LayoutDashboard,
  HardDrive,
  Users,
  Cog,
  Package,
  FileText,
  LogOut,
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
    60000,
  )

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-50 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
        <div className="flex h-14 items-center px-6 gap-4">
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Link to="/dashboard" className="mr-4 flex items-center">
                  <img src="/48.png" alt="Town OS" className="h-8 w-8" />
                </Link>
              </TooltipTrigger>
              <TooltipContent>Home</TooltipContent>
            </Tooltip>
          </TooltipProvider>
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
              <div className="flex items-center rounded-full border border-muted-foreground/30 px-3 py-1.5 animate-pulse">
                <span className="text-sm text-muted-foreground">Loading...</span>
              </div>
            )}
            {ping && ping.status !== 'ok' && (
              <div className="flex items-center rounded-full bg-red-600 px-3 py-1.5">
                <span className="text-sm text-white font-bold">API Offline</span>
              </div>
            )}
            {ping && ping.status === 'ok' && (
              <div className="flex items-center rounded-full border border-muted-foreground/30 px-3 py-1.5">
                <span className="text-sm text-muted-foreground">Online</span>
              </div>
            )}
            {account && (
              <div className="flex items-center gap-1 rounded-full bg-gray-600 px-3 py-1.5">
                <span className="text-sm font-bold text-white">
                  {account.username}
                </span>
                {account.admin && (
                  <Badge variant="secondary" className="ml-1 text-xs">
                    admin
                  </Badge>
                )}
              </div>
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
      <main className="relative mx-auto max-w-6xl px-6 py-6">
        <img
          src="/512.png"
          alt=""
          className="pointer-events-none fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 h-96 w-96 opacity-5"
        />
        {children}
      </main>
    </div>
  )
}
