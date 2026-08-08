import { useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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

// AddEntryDialog collects the name to exempt and an optional reason. The inputs
// are uncontrolled and read back off the form, matching the blocklist dialog.
function AddEntryDialog({ onSubmit, onCancel }) {
  const { t } = useI18n()
  return (
    <Dialog open onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('dns.al.add_entry_title')}</DialogTitle>
          <DialogDescription>{t('dns.al.add_entry_description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor="al-entry-name">
                {t('dns.al.entry_name_label')}
              </label>
              <Input
                id="al-entry-name"
                name="entry_name"
                placeholder={t('dns.al.entry_name_placeholder')}
                required
                className="font-mono"
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium" htmlFor="al-entry-reason">
                {t('dns.al.entry_reason_label')}
              </label>
              <Input
                id="al-entry-reason"
                name="entry_reason"
                placeholder={t('dns.al.entry_reason_placeholder')}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onCancel}>
              {t('dns.cancel_btn')}
            </Button>
            <Button type="submit">{t('dns.al.add_entry')}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// AllowListsTab manages the DNSBL allowlist: names that skip the name-based
// blocklist check entirely, overriding both the configured provider zones and
// the local blocklist. An entry covers the name and every name beneath it.
export default function AllowListsTab({ isAdmin }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [addEntry, setAddEntry] = useState(false)
  const [removeEntry, setRemoveEntry] = useState(null)

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  const [entries] = usePolling(
    () => getClient().listDNSBLAllowlist().catch(() => []),
    [],
    [refreshKey],
    30000,
  )

  async function handleAddEntry(e) {
    e.preventDefault()
    const name = e.target.elements.entry_name.value.trim()
    const reason = e.target.elements.entry_reason.value.trim()
    try {
      await getClient().addDNSBLAllowlist(name, reason)
      toast.success(t('dns.al.entry_added'))
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
      await getClient().removeDNSBLAllowlist(name)
      toast.success(t('dns.al.entry_removed'))
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  const entryColumns = [
    { key: 'name', label: t('dns.al.col_name'), transform: (v) => <span className="font-mono text-sm">{v}</span> },
    { key: 'reason', label: t('dns.al.col_reason'), transform: (v) => <span className="text-sm text-muted-foreground">{v}</span> },
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
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4">
            <div>
              <CardTitle className="text-base">{t('dns.al.title')}</CardTitle>
              <p className="text-sm text-muted-foreground">{t('dns.al.description')}</p>
            </div>
            {isAdmin && (
              <Button variant="outline" size="sm" onClick={() => setAddEntry(true)}>
                <Plus className="h-4 w-4 mr-1" />
                {t('dns.al.add_entry')}
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          <DataTable data={entries} columns={entryColumns} entryKey="name" />
        </CardContent>
      </Card>

      {addEntry && <AddEntryDialog onSubmit={handleAddEntry} onCancel={() => setAddEntry(false)} />}

      <ConfirmDialog
        open={!!removeEntry}
        title={t('dns.al.remove_entry_title')}
        onConfirm={handleRemoveEntry}
        onCancel={() => setRemoveEntry(null)}
        confirmLabel={t('dns.remove_confirm_btn')}
        variant="destructive"
      >
        {t('dns.al.remove_entry_message', { name: removeEntry?.name || '' })}
      </ConfirmDialog>
    </div>
  )
}
