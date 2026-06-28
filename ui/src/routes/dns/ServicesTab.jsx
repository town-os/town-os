import { useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import DataTable from '@/components/DataTable.jsx'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { toast } from 'sonner'

export default function ServicesTab({ isAdmin }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [pending, setPending] = useState({}) // "repo/name" -> bool (in-flight)

  const [services] = usePolling(
    () => getClient().listDNSServices().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  async function toggle(row, published) {
    const key = `${row.repo}/${row.name}`
    setPending((p) => ({ ...p, [key]: true }))
    try {
      await getClient().setDNSService(row.repo, row.name, published)
      toast.success(published ? t('dns.svc.published', { name: row.name }) : t('dns.svc.unpublished', { name: row.name }))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setPending((p) => ({ ...p, [key]: false }))
    }
  }

  const columns = [
    { key: 'name', label: t('dns.svc.col_service'), transform: (v, row) => (
      <div>
        <div className="font-medium text-sm">{v}</div>
        <div className="text-xs text-muted-foreground">{row.repo}</div>
      </div>
    ) },
    { key: 'fqdn', label: t('dns.svc.col_fqdn'), transform: (v) => <span className="font-mono text-sm">{v}</span> },
    { key: 'published', label: t('dns.svc.col_published'), sortable: false, transform: (v, row) => (
      <Switch
        checked={!!v}
        disabled={!isAdmin || pending[`${row.repo}/${row.name}`]}
        onCheckedChange={(next) => toggle(row, next)}
        aria-label={row.fqdn}
      />
    ) },
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t('dns.svc.title')}</CardTitle>
        <p className="text-sm text-muted-foreground">{t('dns.svc.description')}</p>
      </CardHeader>
      <CardContent>
        <DataTable data={services} columns={columns} entryKey="fqdn" />
      </CardContent>
    </Card>
  )
}
