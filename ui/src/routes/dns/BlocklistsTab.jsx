import { useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
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
import { Plus, Trash2 } from 'lucide-react'

// Quick-add zones, queried on demand by rolodex. A zone earns a place here only
// if a household box can actually use it as shipped: still operating, free, and
// answering a self-recursing resolver with no registration step. The operator may
// still type any zone into the custom field — this list is what we vouch for.
//
// Deliberately NOT offered, so nobody re-adds them:
//   dnsbl.sorbs.net       decommissioned 2024-06-05; the zones were emptied, so
//                         it is a permanent no-op that reads as protection.
//   b.barracudacentral.org  free, but requires registering the querying IP first;
//                         an unregistered box may work briefly and then be cut off.
//   uceprotect levels 2/3  list entire ASNs, so one bad neighbour blocks a whole ISP.

// Well-known DNSBL (domain) blocklist zones. This is the side that affects
// ordinary browsing: a listing here answers a forward name lookup.
const DNSBL_SUGGESTIONS = [
  { zone: 'dbl.spamhaus.org', label: 'Spamhaus DBL' },
  { zone: 'multi.surbl.org', label: 'SURBL' },
  { zone: 'black.uribl.com', label: 'URIBL' },
  { zone: 'dbl.nordspam.com', label: 'NordSpam DBL' },
  { zone: 'uribl.spameatingmonkey.net', label: 'Spam Eating Monkey' },
]

// Well-known RBL (IP) blocklist zones. These are only consulted for IPs found in
// reverse DNS queries, which ordinary browsing barely generates — they matter far
// less here than the domain zones above.
const RBL_SUGGESTIONS = [
  { zone: 'zen.spamhaus.org', label: 'Spamhaus ZEN' },
  { zone: 'bl.spamcop.net', label: 'SpamCop' },
  { zone: 'psbl.surriel.com', label: 'PSBL' },
]

// BlocklistProviders renders an RBL or DNSBL provider section: a global enable
// switch, the configured provider zones, a quick-add list of well-known zones,
// and a custom-zone form. Zones are queried on demand by rolodex — nothing is
// fetched or cached. Mutations save the full config immediately and refresh.
function BlocklistProviders({ title, description, config, suggestions, onSave, isAdmin }) {
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

  function addZone(zone) {
    const z = zone.trim().toLowerCase()
    if (!z) return
    if (providers.some((p) => p.zone === z)) {
      toast.error(t('dns.bl.duplicate_zone'))
      return
    }
    save(enabled, [...providers, { zone: z, enabled: true }])
  }

  const unconfigured = suggestions.filter((s) => !providers.some((p) => p.zone === s.zone))

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

        {isAdmin && unconfigured.length > 0 && (
          <div className="pt-1">
            <p className="text-xs text-muted-foreground mb-2">{t('dns.bl.suggested')}</p>
            <div className="flex flex-wrap gap-2">
              {unconfigured.map((s) => (
                <Button key={s.zone} variant="outline" size="sm" onClick={() => addZone(s.zone)}>
                  <Plus className="h-3 w-3 mr-1" />
                  {s.label}
                </Button>
              ))}
            </div>
          </div>
        )}

        {isAdmin && (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              addZone(newZone)
              setNewZone('')
            }}
            className="flex items-center gap-2 pt-2"
          >
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
  const [addEntry, setAddEntry] = useState(false)
  const [removeEntry, setRemoveEntry] = useState(null)

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  const [dnsbl] = usePolling(
    () => getClient().getDNSBLConfig().catch(() => null),
    null,
    [refreshKey],
    60000,
  )
  const [rbl] = usePolling(
    () => getClient().getRBLConfig().catch(() => null),
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
      <BlocklistProviders
        title={t('dns.bl.dnsbl_title')}
        description={t('dns.bl.dnsbl_description')}
        config={dnsbl}
        suggestions={DNSBL_SUGGESTIONS}
        isAdmin={isAdmin}
        onSave={async (enabled, providers) => {
          await getClient().setDNSBLConfig(enabled, providers)
          doRefresh()
        }}
      />
      <BlocklistProviders
        title={t('dns.bl.rbl_title')}
        description={t('dns.bl.rbl_description')}
        config={rbl}
        suggestions={RBL_SUGGESTIONS}
        isAdmin={isAdmin}
        onSave={async (enabled, providers) => {
          await getClient().setRBLConfig(enabled, providers)
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
    </div>
  )
}
