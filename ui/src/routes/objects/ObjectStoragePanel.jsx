import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { GRANT_GFEH } from '@/lib/grants.js'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import OverviewTab from './OverviewTab.jsx'
import UsersTab from './UsersTab.jsx'
import GrantsTab from './GrantsTab.jsx'
import LinksTab from './LinksTab.jsx'

/**
 * Object storage: one gfeh partition per network.
 *
 * The body is shared by two places, which is why it is a panel and not a page:
 * its own screen at /dashboard/objects, and a section of the services screen.
 * The partitions are system services -- one `town-os-system--gfeh-<network>`
 * unit each -- so they belong beside the row that says whether the daemon is
 * running, while still being reachable directly.
 *
 * The network selector is the primary axis rather than a filter, because a
 * partition IS a network here: its users, its grants and its published
 * addresses are all scoped to one, and showing them merged would invite
 * granting somebody access on the wrong one.
 *
 * @param {object} props
 * @param {object|null} props.account the viewing account, for the manage checks
 * @param {string} [props.tabParam] query-string key holding the active sub-tab.
 *   Defaults to `tab`, which is what the standalone page's links have always
 *   used. The services screen passes something else because it already spends
 *   `?tab=` and `?expand=` on its own state.
 */
export default function ObjectStoragePanel({ account, tabParam = 'tab' }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)

  // Re-read the caller's own account rather than trusting the copy stored at
  // login: the kind is set by somebody else, on a session that is already open,
  // and an operator who has just been made network-only should not have to be
  // told to log out and back in before the buttons appear. The stored copy is
  // the fallback while that request is in flight or failing.
  const [me] = usePolling(
    () =>
      account?.username
        ? getClient().getAccount(account.username).catch(() => null)
        : Promise.resolve(null),
    null,
    [account?.username, refreshKey],
    60000,
  )
  const effective = me || account
  // An administrator holds every grant. Anybody else needs the gfeh grant --
  // and the server already returns only the partitions their networks own, so
  // anything selectable here is one they may act on. Exactly the set
  // requireObjectStorage admits: the buttons this hides are the requests that
  // would 403.
  const canManage = !!effective?.admin || (effective?.grants || []).includes(GRANT_GFEH)
  const [searchParams, setSearchParams] = useSearchParams()

  const [partitions, , loading] = usePolling(
    () => getClient().listGfehPartitions().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  // The selected partition lives in the URL alongside the tab, so a link to a
  // particular partition's grants is a link somebody can send.
  const activeTab = searchParams.get(tabParam) || 'overview'
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

  // Object storage has no switch, so an empty list is not "switched off": it is
  // a box whose partitions have not been provisioned, or one built without
  // them. Say that rather than offering a control that no longer exists.
  if (partitions.length === 0) {
    return (
      <Alert>
        <AlertDescription>{t('objects.not_configured')}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <p className="text-sm text-muted-foreground">{t('objects.description')}</p>
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

      <Tabs value={activeTab} onValueChange={(v) => setParam(tabParam, v)}>
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
          <UsersTab network={selected} canManage={canManage} onChanged={doRefresh} />
        </TabsContent>
        <TabsContent value="grants" className="mt-4">
          <GrantsTab network={selected} canManage={canManage} />
        </TabsContent>
        <TabsContent value="links" className="mt-4">
          <LinksTab network={selected} canManage={canManage} />
        </TabsContent>
      </Tabs>
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
