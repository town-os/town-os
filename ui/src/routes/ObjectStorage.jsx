import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Database } from 'lucide-react'

import getClient from '@/lib/client-instance.js'
import { usePolling, useRequireAuth } from '@/lib/hooks.js'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import OverviewTab from './objects/OverviewTab.jsx'
import UsersTab from './objects/UsersTab.jsx'
import GrantsTab from './objects/GrantsTab.jsx'
import LinksTab from './objects/LinksTab.jsx'

/**
 * Object storage: one gfeh partition per network.
 *
 * The network selector is the primary axis rather than a filter, because a
 * partition IS a network here — its users, its grants and its published
 * addresses are all scoped to one, and showing them merged would invite
 * granting somebody access on the wrong one.
 */
export default function ObjectStorage() {
  const { t } = useI18n()
  const account = useRequireAuth()
  const isAdmin = !!account?.admin

  const [refreshKey, setRefreshKey] = useState(0)
  const [searchParams, setSearchParams] = useSearchParams()

  useEffect(() => {
    document.title = t('objects.page_title')
  }, [t])

  const [partitions, , loading] = usePolling(
    () => getClient().listGfehPartitions().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  // The selected partition lives in the URL alongside the tab, so a link to a
  // particular partition's grants is a link somebody can send.
  const activeTab = searchParams.get('tab') || 'overview'
  const selected = searchParams.get('network') || partitions[0]?.network || ''

  function setParam(key, value) {
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev)
        p.set(key, value)
        return p
      },
      { replace: true },
    )
  }

  const partition = partitions.find((p) => p.network === selected) || null

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  if (loading) {
    return <div className="h-32 animate-pulse rounded-md bg-muted" />
  }

  if (partitions.length === 0) {
    return (
      <div className="space-y-4">
        <Header t={t} />
        <Alert>
          <AlertDescription>{t('objects.not_configured')}</AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <Header t={t} />
        <Select value={selected} onValueChange={(v) => setParam('network', v)}>
          <SelectTrigger className="w-56">
            <SelectValue placeholder={t('objects.select_network')} />
          </SelectTrigger>
          <SelectContent>
            {partitions.map((p) => (
              <SelectItem key={p.network} value={p.network}>
                {p.network}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {partition && <PartitionSummary partition={partition} t={t} />}

      <Tabs value={activeTab} onValueChange={(v) => setParam('tab', v)}>
        <TabsList>
          <TabsTrigger value="overview">{t('objects.tab_overview')}</TabsTrigger>
          <TabsTrigger value="users">{t('objects.tab_users')}</TabsTrigger>
          <TabsTrigger value="grants">{t('objects.tab_grants')}</TabsTrigger>
          <TabsTrigger value="links">{t('objects.tab_links')}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <OverviewTab partition={partition} />
        </TabsContent>
        <TabsContent value="users" className="mt-4">
          <UsersTab network={selected} isAdmin={isAdmin} onChanged={doRefresh} />
        </TabsContent>
        <TabsContent value="grants" className="mt-4">
          <GrantsTab network={selected} isAdmin={isAdmin} />
        </TabsContent>
        <TabsContent value="links" className="mt-4">
          <LinksTab network={selected} isAdmin={isAdmin} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function Header({ t }) {
  return (
    <div>
      <h1 className="flex items-center gap-2 text-2xl font-semibold">
        <Database className="h-6 w-6" />
        {t('objects.title')}
      </h1>
      <p className="mt-1 text-sm text-muted-foreground">{t('objects.description')}</p>
    </div>
  )
}

/**
 * A partition's state at a glance.
 *
 * Running is its own line rather than an absence, because a partition that
 * exists but is not answering is a real and distinct condition: its data is
 * still there, its addresses are just not being published.
 */
function PartitionSummary({ partition, t }) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-3 text-base">
          {partition.network}
          {partition.running ? (
            <Badge>{t('objects.running')}</Badge>
          ) : (
            <Badge variant="destructive">{t('objects.stopped')}</Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex gap-8 pt-0 text-sm text-muted-foreground">
        <span>
          {t('objects.network')}: <code className="font-mono">{partition.tld}</code>
        </span>
        <span>
          {t('objects.quota')}: {partition.quota > 0 ? formatBytes(partition.quota) : t('objects.unlimited')}
        </span>
      </CardContent>
    </Card>
  )
}

/** Human-readable byte size. */
function formatBytes(bytes) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value % 1 === 0 ? value : value.toFixed(1)} ${units[unit]}`
}
