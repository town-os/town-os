import { useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { useRequireAuth } from '@/lib/hooks.js'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
  Wifi,
  Network,
  AlertTriangle,
} from 'lucide-react'

const NAV_KEYS = [
  { to: '/dashboard', key: 'nav.home', icon: LayoutDashboard },
  { to: '/dashboard/storage', key: 'nav.storage', icon: HardDrive },
  { to: '/dashboard/users', key: 'nav.users', icon: Users },
  { to: '/dashboard/system', key: 'nav.services', icon: Cog },
  { to: '/dashboard/packages', key: 'nav.packages', icon: Package },
  { to: '/dashboard/pages', key: 'nav.pages', icon: Globe },
  { to: '/dashboard/dns', key: 'nav.dns', icon: Wifi },
  { to: '/dashboard/networks', key: 'nav.networks', icon: Network, adminOnly: true },
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
  }, [ping?.locale, locale, setLocale])

  return (
    <div className="flex min-h-screen bg-background">
      <aside className="sticky top-0 h-screen w-56 flex-shrink-0 border-r bg-background flex flex-col">
        <div className="flex h-14 items-center px-4">
          <Link to="/dashboard" className="flex w-full items-center justify-center gap-2 rounded-sm px-3 py-1.5 bg-accent border border-solid border-black">
            <img src="/48.png" alt={t('nav.logo_alt')} className="h-8 w-8" />
            <span className="text-lg font-semibold tracking-normal text-black" style={{ fontFamily: '"Raleway", "Montserrat", "Poppins", sans-serif' }}>Town OS</span>
          </Link>
        </div>
        <nav className="flex flex-col gap-1 px-3 py-2">
          {/* Pages is unconditional: the pages subsystem is always
              initialized at boot (there is no feature gate), so hiding
              the entry behind a ping field only made it pop in a beat
              after the rest of the sidebar had already rendered. */}
          {NAV_KEYS.filter((item) => !item.adminOnly || account?.admin).map((navItem) => {
            const NavIcon = navItem.icon
            const active = location.pathname === navItem.to
            return (
              <Button
                key={navItem.to}
                variant={active ? 'secondary' : 'ghost'}
                size="sm"
                className="w-full justify-start"
                asChild
              >
                <Link to={navItem.to}>
                  <NavIcon className="h-4 w-4 mr-2" />
                  {t(navItem.key)}
                </Link>
              </Button>
            )
          })}
        </nav>
      </aside>
      <div className="flex-1 flex flex-col min-w-0">
        <header className="sticky top-0 z-50 flex h-14 items-center justify-end border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60 px-6 gap-3">
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
        </header>
        <main className="relative mx-auto w-full max-w-6xl px-6 py-6">
          <img
            src="/512.png"
            alt=""
            className="pointer-events-none absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 h-96 w-96 opacity-5"
          />
          {ping && ping.status !== 'ok' && (
            <Alert variant="destructive" className="mb-6">
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription>
                {t('nav.api_offline_message')}
              </AlertDescription>
            </Alert>
          )}
          {children}
        </main>
      </div>
    </div>
  )
}
