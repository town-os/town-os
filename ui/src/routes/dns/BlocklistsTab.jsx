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
import { Plus, Trash2, ShieldAlert, SlidersHorizontal } from 'lucide-react'

// Rolodex's built-in refusal codes, mirrored from DEFAULT_REFUSAL_CODES in its
// src/dnsbl.rs (named src/rbl.rs before the RBL half was retired). This copy is
// used for ONE thing: deciding whether a provider's resolved codes are the
// untouched built-in set, so the settings dialog opens on the right choice. It is never sent — "use the built-ins" is expressed by
// sending no codes at all, precisely so the box tracks rolodex as it adds codes
// rather than freezing today's list into every stored config.
//
// If it drifts from rolodex, the dialog opens on "Custom" prefilled with the
// codes actually in effect. That is a cosmetic wrong default, not a wrong
// configuration — nothing changes unless the operator saves.
const BUILTIN_REFUSAL_CODES = [
  '127.255.255.0/24',
  '127.0.1.255',
  '127.0.2.255',
  '127.0.0.1',
  '127.0.0.255',
]

// The single-entry spelling that switches refusal detection off. An empty list
// cannot mean that: an empty list is what every config written before refusal
// handling existed already has, and it means "use the built-in set".
const REFUSAL_CODES_NONE = 'none'

// refusalMode reads a provider's RESOLVED codes (what GET reports as actually in
// effect) back into the choice that produced them.
function refusalMode(codes) {
  const list = codes || []
  if (list.length === 1 && list[0].toLowerCase() === REFUSAL_CODES_NONE) return 'off'
  if (list.length === 0) return 'builtin'
  const a = [...list].sort()
  const b = [...BUILTIN_REFUSAL_CODES].sort()
  return a.length === b.length && a.every((v, i) => v === b[i]) ? 'builtin' : 'custom'
}

// formatRemaining renders a rotate-out countdown as minutes once it is long
// enough that seconds are noise. An hour-long backoff shown as "3212s" makes the
// reader do arithmetic to answer the only question they have: is this a blip or
// is my blocklist off for the afternoon.
function formatRemaining(secs) {
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.round(secs / 60)}m`
  const h = Math.floor(secs / 3600)
  const m = Math.round((secs % 3600) / 60)
  return m ? `${h}h ${m}m` : `${h}h`
}

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
//
// There is no second list. The RBL (reverse-IP) half is retired upstream — the
// provider lookup, its config RPCs and the `/dns/rbl` endpoints are all gone —
// so a quick-add list of `zen.spamhaus.org` and friends would offer zones
// nothing can query. What survives is the LOCAL blocklist below, which still
// takes an address.
const DNSBL_SUGGESTIONS = [
  { zone: 'dbl.spamhaus.org', label: 'Spamhaus DBL' },
  { zone: 'multi.surbl.org', label: 'SURBL' },
  { zone: 'black.uribl.com', label: 'URIBL' },
  { zone: 'dbl.nordspam.com', label: 'NordSpam DBL' },
  { zone: 'uribl.spameatingmonkey.net', label: 'Spam Eating Monkey' },
]

// RefusalDialog edits one provider's answer to "what does a refusal look like
// coming back from you".
//
// A blocklist answers a listing and a complaint about the querier with the same
// kind of record — an A in 127.0.0.0/8 — so only the address separates "this
// name is malicious" from "you are over your query limit". Reading the latter
// as a listing NXDOMAINs every name checked against that provider, turning the
// blocklist into an outage, which is what a household box quietly exceeding
// Spamhaus's free-use limit runs into.
function RefusalDialog({ provider, listCooldown, onSubmit, onCancel }) {
  const { t } = useI18n()
  const inEffect = provider.refusal_codes || []
  const [mode, setMode] = useState(refusalMode(inEffect))
  const [codes, setCodes] = useState(
    refusalMode(inEffect) === 'custom' ? inEffect.join('\n') : '',
  )
  const [cooldown, setCooldown] = useState(String(provider.refusal_cooldown_secs || 0))

  function submit(e) {
    e.preventDefault()
    let next
    if (mode === 'builtin') next = []
    else if (mode === 'off') next = [REFUSAL_CODES_NONE]
    else next = codes.split(/[\s,]+/).map((c) => c.trim()).filter(Boolean)

    const secs = Number(cooldown)
    if (!Number.isInteger(secs) || secs < 0) {
      toast.error(t('dns.bl.refusal_cooldown_invalid'))
      return
    }
    onSubmit({ refusal_codes: next, refusal_cooldown_secs: secs })
  }

  const modes = [
    { value: 'builtin', label: t('dns.bl.refusal_mode_builtin'), hint: t('dns.bl.refusal_mode_builtin_hint') },
    { value: 'custom', label: t('dns.bl.refusal_mode_custom'), hint: t('dns.bl.refusal_mode_custom_hint') },
    { value: 'off', label: t('dns.bl.refusal_mode_off'), hint: t('dns.bl.refusal_mode_off_hint') },
  ]

  return (
    <Dialog open onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('dns.bl.refusal_title', { zone: provider.zone })}</DialogTitle>
          <DialogDescription>{t('dns.bl.refusal_description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              {modes.map((m) => (
                <label key={m.value} className="flex items-start gap-2 text-sm">
                  <input
                    type="radio"
                    name="refusal_mode"
                    value={m.value}
                    checked={mode === m.value}
                    onChange={() => setMode(m.value)}
                    className="mt-1"
                  />
                  <span>
                    <span className="font-medium">{m.label}</span>
                    <span className="block text-xs text-muted-foreground">{m.hint}</span>
                  </span>
                </label>
              ))}
            </div>

            {mode === 'custom' && (
              <div className="space-y-2">
                <label className="text-sm font-medium" htmlFor="refusal-codes">
                  {t('dns.bl.refusal_codes_label')}
                </label>
                <Input
                  id="refusal-codes"
                  value={codes}
                  onChange={(e) => setCodes(e.target.value)}
                  placeholder={t('dns.bl.refusal_codes_placeholder')}
                  className="font-mono"
                />
              </div>
            )}

            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor="refusal-cooldown">
                {t('dns.bl.refusal_cooldown_label')}
              </label>
              <Input
                id="refusal-cooldown"
                type="number"
                min="0"
                value={cooldown}
                onChange={(e) => setCooldown(e.target.value)}
                className="font-mono"
              />
              <p className="text-xs text-muted-foreground">
                {t('dns.bl.refusal_cooldown_hint', { secs: listCooldown || 3600 })}
              </p>
            </div>

            {inEffect.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {t('dns.bl.refusal_in_effect', { codes: inEffect.join(', ') })}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onCancel}>
              {t('dns.cancel_btn')}
            </Button>
            <Button type="submit">{t('dns.bl.refusal_save')}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// RotatedOut reports the providers rolodex is currently NOT asking, because they
// refused a query. Without this the only signal that a blocklist stopped being
// consulted is that it stopped blocking things — which reads as the blocklist
// being useless rather than as the box having been rate-limited.
function RotatedOut({ entries }) {
  const { t } = useI18n()
  if (!entries || entries.length === 0) return null
  return (
    <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 space-y-1">
      <div className="flex items-center gap-2 text-sm font-medium">
        <ShieldAlert className="h-4 w-4" />
        {t('dns.bl.rotated_out_title')}
      </div>
      <p className="text-xs text-muted-foreground">{t('dns.bl.rotated_out_description')}</p>
      {entries.map((r) => (
        <p key={r.zone} className="text-xs">
          {t('dns.bl.rotated_out_entry', {
            zone: r.zone,
            code: r.code,
            remaining: formatRemaining(r.seconds_remaining || 0),
          })}
        </p>
      ))}
    </div>
  )
}

// BlocklistProviders renders the DNSBL provider section: a global enable
// switch, the configured provider zones, a quick-add list of well-known zones,
// and a custom-zone form. Zones are queried on demand by rolodex — nothing is
// fetched or cached. Mutations save the full config immediately and refresh.
function BlocklistProviders({ title, description, config, suggestions, onSave, isAdmin }) {
  const { t } = useI18n()
  const [newZone, setNewZone] = useState('')
  const [editRefusal, setEditRefusal] = useState(null)
  const providers = config?.providers || []
  const enabled = !!config?.enabled
  const listCooldown = config?.refusal_cooldown_secs || 0

  // toWire strips a provider's RESOLVED codes back to the choice that produced
  // them. GET reports what is actually in effect, so a provider that named no
  // codes reads back carrying the built-in set — and echoing that straight back
  // on the next save would freeze today's list into the stored config, so a code
  // rolodex adds later would start being read as a listing. The one thing that
  // must survive a round trip is "I said nothing", and that is an absent field.
  function toWire(p) {
    const codes = refusalMode(p.refusal_codes) === 'builtin' ? [] : (p.refusal_codes || [])
    return {
      zone: p.zone,
      enabled: p.enabled,
      ...(codes.length ? { refusal_codes: codes } : {}),
      ...(p.refusal_cooldown_secs ? { refusal_cooldown_secs: p.refusal_cooldown_secs } : {}),
    }
  }

  async function save(nextEnabled, nextProviders) {
    try {
      await onSave(nextEnabled, nextProviders.map(toWire), listCooldown)
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

  // saveRefusal replaces one provider's refusal settings with the dialog's
  // answer. It still goes through toWire, which is harmless here: the dialog's
  // "use the built-in codes" is already an empty list, and that is exactly what
  // toWire collapses a resolved built-in set back to.
  function saveRefusal(zone, next) {
    setEditRefusal(null)
    save(
      enabled,
      providers.map((p) => (p.zone === zone ? { ...p, ...next } : p)),
    )
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
        <RotatedOut entries={config?.rotated_out} />

        {providers.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('dns.bl.no_providers')}</p>
        )}
        {providers.map((p) => (
          <div key={p.zone} className="flex items-center gap-3">
            <span className="font-mono text-sm flex-1">
              {p.zone}
              {refusalMode(p.refusal_codes) === 'off' && (
                <span className="ml-2 text-xs font-sans text-muted-foreground">
                  {t('dns.bl.refusal_off_badge')}
                </span>
              )}
            </span>
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
                onClick={() => setEditRefusal(p)}
                aria-label={t('dns.bl.refusal_button_label', { zone: p.zone })}
                title={t('dns.bl.refusal_button')}
              >
                <SlidersHorizontal className="h-3 w-3" />
              </Button>
            )}
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

        {editRefusal && (
          <RefusalDialog
            provider={editRefusal}
            listCooldown={listCooldown}
            onSubmit={(next) => saveRefusal(editRefusal.zone, next)}
            onCancel={() => setEditRefusal(null)}
          />
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
        onSave={async (enabled, providers, refusalCooldownSecs) => {
          await getClient().setDNSBLConfig(enabled, providers, refusalCooldownSecs)
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
