import { useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { toast } from 'sonner'
import { Plus, Trash2, Loader2 } from 'lucide-react'

// BlocklistProviders renders an RBL or DNSBL provider section: a global enable
// switch and a list of provider zones. Mutations save the full config
// immediately and refresh.
function BlocklistProviders({ title, description, config, onSave, isAdmin }) {
  const { t } = useI18n()
  const [newZone, setNewZone] = useState('')
  const providers = config?.providers || []
  const enabled = !!config?.enabled

  async function save(nextEnabled, nextProviders) {
    try {
      await onSave(nextEnabled, nextProviders)
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  function addZone(e) {
    e.preventDefault()
    const zone = newZone.trim().toLowerCase()
    if (!zone) return
    if (providers.some((p) => p.zone === zone)) {
      toast.error(t('dns.bl.duplicate_zone'))
      return
    }
    setNewZone('')
    save(enabled, [...providers, { zone, enabled: true }])
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div>
            <CardTitle className="text-base">{title}</CardTitle>
            <p className="text-sm text-muted-foreground">{description}</p>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {enabled ? t('dns.bl.enabled') : t('dns.bl.disabled')}
            </span>
            <Switch
              checked={enabled}
              disabled={!isAdmin}
              onCheckedChange={(v) => save(v, providers)}
              aria-label={title}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {providers.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('dns.bl.no_providers')}</p>
        )}
        {providers.map((p) => (
          <div key={p.zone} className="flex items-center gap-3">
            <span className="font-mono text-sm flex-1">{p.zone}</span>
            <Switch
              checked={p.enabled}
              disabled={!isAdmin}
              onCheckedChange={(v) =>
                save(enabled, providers.map((q) => (q.zone === p.zone ? { ...q, enabled: v } : q)))
              }
              aria-label={p.zone}
            />
            {isAdmin && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => save(enabled, providers.filter((q) => q.zone !== p.zone))}
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            )}
          </div>
        ))}
        {isAdmin && (
          <form onSubmit={addZone} className="flex items-center gap-2 pt-2">
            <Input
              value={newZone}
              onChange={(e) => setNewZone(e.target.value)}
              placeholder={t('dns.bl.zone_placeholder')}
              className="font-mono"
            />
            <Button type="submit" variant="outline" size="sm">
              <Plus className="h-4 w-4 mr-1" />
              {t('dns.bl.add_zone')}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

// AddEntryDialog is a small dialog with a form body for adding a local entry.
function AddEntryDialog({ onSubmit, onCancel }) {
  const { t } = useI18n()
  return (
    <Dialog open onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('dns.bl.add_entry_title')}</DialogTitle>
          <DialogDescription>{t('dns.bl.add_entry_description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor="bl-entry-name">
                {t('dns.bl.entry_name_label')}
              </label>
              <Input id="bl-entry-name" name="entry_name" placeholder="ads.example.com" required className="font-mono" />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor="bl-entry-reason">
                {t('dns.bl.entry_reason_label')}
              </label>
              <Input id="bl-entry-reason" name="entry_reason" placeholder={t('dns.bl.entry_reason_placeholder')} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onCancel}>
              {t('dns.cancel_btn')}
            </Button>
            <Button type="submit">{t('dns.bl.add_entry')}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export default function BlocklistsTab({ isAdmin }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [selected, setSelected] = useState({}) // feed key -> bool
  const [addEntry, setAddEntry] = useState(false)
  const [removeEntry, setRemoveEntry] = useState(null)
  const [clearConfirm, setClearConfirm] = useState(false)

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  const [blocklists] = usePolling(
    () => getClient().listBlocklists().catch(() => null),
    null,
    [refreshKey],
    5000,
  )
  const [rbl] = usePolling(
    () => getClient().getRBLConfig().catch(() => null),
    null,
    [refreshKey],
    60000,
  )
  const [dnsbl] = usePolling(
    () => getClient().getDNSBLConfig().catch(() => null),
    null,
    [refreshKey],
    60000,
  )
  const [localEntries] = usePolling(
    () => getClient().listLocalRBL().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  const feeds = blocklists?.feeds || []
  const running = !!blocklists?.running
  const statusByKey = Object.fromEntries((blocklists?.status || []).map((s) => [s.key, s]))
  const selectedKeys = feeds.filter((f) => selected[f.key]).map((f) => f.key)

  async function applyFeeds(keys) {
    if (keys.length === 0) {
      toast.error(t('dns.bl.select_one'))
      return
    }
    try {
      await getClient().applyBlocklists({ keys })
      toast.success(t('dns.bl.apply_started'))
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function clearAll() {
    setClearConfirm(false)
    try {
      const res = await getClient().clearBlocklists([])
      toast.success(t('dns.bl.cleared', { count: res?.removed ?? 0 }))
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleAddEntry(e) {
    e.preventDefault()
    const name = e.target.elements.entry_name.value.trim()
    const reason = e.target.elements.entry_reason.value.trim()
    try {
      await getClient().addLocalRBL(name, reason)
      toast.success(t('dns.bl.entry_added'))
      setAddEntry(false)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleRemoveEntry() {
    if (!removeEntry) return
    const name = removeEntry.name
    setRemoveEntry(null)
    try {
      await getClient().removeLocalRBL(name)
      toast.success(t('dns.bl.entry_removed'))
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  const entryColumns = [
    { key: 'name', label: t('dns.bl.col_name'), transform: (v) => <span className="font-mono text-sm">{v}</span> },
    { key: 'reason', label: t('dns.bl.col_reason'), transform: (v) => <span className="text-sm text-muted-foreground">{v}</span> },
    ...(isAdmin ? [{
      key: '_actions', label: t('dns.col_actions'), sortable: false,
      transform: (_, row) => (
        <Button variant="ghost" size="sm" onClick={() => setRemoveEntry(row)}>
          <Trash2 className="h-3 w-3" />
        </Button>
      ),
    }] : []),
  ]

  return (
    <div className="space-y-6">
      {/* Curated blocklist feeds */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4 flex-wrap">
            <div>
              <CardTitle className="text-base">{t('dns.bl.feeds_title')}</CardTitle>
              <p className="text-sm text-muted-foreground">{t('dns.bl.feeds_description')}</p>
            </div>
            {isAdmin && (
              <div className="flex items-center gap-2">
                {running && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
                <Button variant="outline" size="sm" disabled={running} onClick={() => applyFeeds(selectedKeys)}>
                  {t('dns.bl.apply_selected')}
                </Button>
                <Button variant="outline" size="sm" disabled={running} onClick={() => applyFeeds(feeds.map((f) => f.key))}>
                  {t('dns.bl.apply_all')}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setClearConfirm(true)}>
                  {t('dns.bl.clear_all')}
                </Button>
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {feeds.map((f) => {
            const st = statusByKey[f.key]
            return (
              <div key={f.key} className="flex items-start gap-3">
                <Switch
                  checked={!!selected[f.key]}
                  disabled={!isAdmin}
                  onCheckedChange={(v) => setSelected((s) => ({ ...s, [f.key]: v }))}
                  aria-label={f.name}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-sm">{f.name}</span>
                    {st?.done && !st?.error && (
                      <Badge variant="secondary">{t('dns.bl.loaded', { count: st.added })}</Badge>
                    )}
                    {st && !st.done && (
                      <Badge variant="secondary">
                        {t('dns.bl.loading_count', { added: st.added, total: st.total })}
                      </Badge>
                    )}
                    {st?.error && <Badge variant="destructive">{t('dns.bl.failed')}</Badge>}
                  </div>
                  <p className="text-sm text-muted-foreground">{f.description}</p>
                </div>
              </div>
            )
          })}
        </CardContent>
      </Card>

      {/* RBL / DNSBL provider zones */}
      <BlocklistProviders
        title={t('dns.bl.rbl_title')}
        description={t('dns.bl.rbl_description')}
        config={rbl}
        isAdmin={isAdmin}
        onSave={async (enabled, providers) => {
          await getClient().setRBLConfig(enabled, providers)
          doRefresh()
        }}
      />
      <BlocklistProviders
        title={t('dns.bl.dnsbl_title')}
        description={t('dns.bl.dnsbl_description')}
        config={dnsbl}
        isAdmin={isAdmin}
        onSave={async (enabled, providers) => {
          await getClient().setDNSBLConfig(enabled, providers)
          doRefresh()
        }}
      />

      {/* Local blocklist entries */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4">
            <div>
              <CardTitle className="text-base">{t('dns.bl.local_title')}</CardTitle>
              <p className="text-sm text-muted-foreground">{t('dns.bl.local_description')}</p>
            </div>
            {isAdmin && (
              <Button variant="outline" size="sm" onClick={() => setAddEntry(true)}>
                <Plus className="h-4 w-4 mr-1" />
                {t('dns.bl.add_entry')}
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          <DataTable data={localEntries} columns={entryColumns} entryKey="name" />
        </CardContent>
      </Card>

      {addEntry && <AddEntryDialog onSubmit={handleAddEntry} onCancel={() => setAddEntry(false)} />}

      <ConfirmDialog
        open={!!removeEntry}
        title={t('dns.bl.remove_entry_title')}
        onConfirm={handleRemoveEntry}
        onCancel={() => setRemoveEntry(null)}
        confirmLabel={t('dns.remove_confirm_btn')}
        variant="destructive"
      >
        {t('dns.bl.remove_entry_message', { name: removeEntry?.name || '' })}
      </ConfirmDialog>

      <ConfirmDialog
        open={clearConfirm}
        title={t('dns.bl.clear_title')}
        onConfirm={clearAll}
        onCancel={() => setClearConfirm(false)}
        confirmLabel={t('dns.bl.clear_all')}
        variant="destructive"
      >
        {t('dns.bl.clear_message')}
      </ConfirmDialog>
    </div>
  )
}
