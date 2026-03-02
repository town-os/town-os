import { useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useI18n } from '@/i18n/I18nContext.jsx'
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
  Globe,
  LogOut,
  Settings,
  Activity,
} from 'lucide-react'

const NAV_KEYS = [
  { to: '/dashboard', key: 'nav.home', icon: LayoutDashboard },
  { to: '/dashboard/storage', key: 'nav.storage', icon: HardDrive },
  { to: '/dashboard/users', key: 'nav.users', icon: Users },
  { to: '/dashboard/system', key: 'nav.services', icon: Cog },
  { to: '/dashboard/packages', key: 'nav.packages', icon: Package },
  { to: '/dashboard/pages', key: 'nav.pages', icon: Globe },
  { to: '/dashboard/monitoring', key: 'nav.monitoring', icon: Activity },
  { to: '/dashboard/log', key: 'nav.audit_log', icon: FileText },
  { to: '/dashboard/settings', key: 'nav.settings', icon: Settings, adminOnly: true },
]

export default function Dashboard({ children }) {
  const { t, locale, setLocale } = useI18n()
  const account = useRequireAuth()
  const location = useLocation()

  const [ping, , loading] = usePolling(
    () => getClient().ping().catch(() => ({ status: 'error' })),
    null,
    [],
    60000,
  )

  useEffect(() => {
    if (ping?.locale && ping.locale !== locale) {
      setLocale(ping.locale)
    }
  }, [ping?.locale])

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-50 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
        <div className="flex h-14 items-center px-6 gap-4">
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Link to="/dashboard" className="mr-4 flex items-center">
                  <img src="/48.png" alt={t('nav.logo_alt')} className="h-8 w-8" />
                </Link>
              </TooltipTrigger>
              <TooltipContent>{t('nav.home_tooltip')}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
          <Separator orientation="vertical" className="h-6" />
          <nav className="flex items-center gap-1">
            {NAV_KEYS.filter(
              (item) => !item.adminOnly || account?.admin,
            ).map(({ to, key, icon: Icon }) => {
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
                    {t(key)}
                  </Link>
                </Button>
              )
            })}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            {loading && !ping && (
              <div className="flex items-center rounded-full border border-muted-foreground/30 px-3 py-1.5 animate-pulse">
                <span className="text-sm text-muted-foreground">{t('nav.loading')}</span>
              </div>
            )}
            {ping && ping.status !== 'ok' && (
              <div className="flex items-center rounded-full bg-red-600 px-3 py-1.5">
                <span className="text-sm text-white font-bold">{t('nav.api_offline')}</span>
              </div>
            )}
            {ping && ping.status === 'ok' && (
              <div className="flex items-center rounded-full border border-muted-foreground/30 px-3 py-1.5">
                <span className="text-sm text-muted-foreground">{t('nav.online')}</span>
              </div>
            )}
            {account && (
              <div className="flex items-center gap-1 rounded-full bg-gray-600 px-3 py-1.5">
                <span className="text-sm font-bold text-white">
                  {account.username}
                </span>
                {account.admin && (
                  <Badge variant="secondary" className="ml-1 text-xs">
                    {t('nav.admin_badge')}
                  </Badge>
                )}
              </div>
            )}
            <Button variant="ghost" size="sm" asChild>
              <Link to="/logout">
                <LogOut className="h-4 w-4 mr-1" />
                {t('nav.logout')}
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
