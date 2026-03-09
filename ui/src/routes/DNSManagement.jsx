import { useState, useEffect } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { useRequireAuth, usePolling } from '@/lib/hooks.js'
import getClient from '@/lib/client-instance.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Plus, Trash2 } from 'lucide-react'

const RECORD_TYPES = {
  0: 'A',
  1: 'AAAA',
  2: 'CNAME',
  3: 'MX',
  4: 'TXT',
  5: 'NS',
  6: 'SOA',
  7: 'SRV',
  8: 'PTR',
  9: 'URI',
  10: 'SSHFP',
  11: 'DNAME',
  12: 'ANAME',
}

const RECORD_TYPE_BY_NAME = Object.fromEntries(
  Object.entries(RECORD_TYPES).map(([k, v]) => [v, Number(k)])
)

const ADD_RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'SRV', 'PTR']

export default function DNSManagement() {
  const { t } = useI18n()
  const account = useRequireAuth()
  useEffect(() => { document.title = t('dns.page_title') }, [t])

  const [refreshKey, setRefreshKey] = useState(0)
  const [addDialog, setAddDialog] = useState(false)
  const [changeTLDDialog, setChangeTLDDialog] = useState(false)
  const [setupConfirm, setSetupConfirm] = useState(false)
  const [removeConfirm, setRemoveConfirm] = useState(null)
  const [addRecordType, setAddRecordType] = useState('')

  const [status] = usePolling(
    () => getClient().dnsStatus().catch(() => null),
    null,
    [refreshKey],
    15000,
  )

  const [records, , recordsLoading] = usePolling(
    () => getClient().listDNSRecords().catch(() => []),
    [],
    [refreshKey],
    15000,
  )

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleAddRecord(e) {
    e.preventDefault()
    const form = e.target.elements
    const name = form.record_name.value.trim()
    const value = form.record_value.value.trim()
    const ttl = parseInt(form.record_ttl.value, 10) || 300
    const recordType = RECORD_TYPE_BY_NAME[addRecordType]

    try {
      await getClient().addDNSRecord(name, recordType, value, ttl)
      toast.success(t('dns.toast_record_added'))
      setAddDialog(false)
      setAddRecordType('')
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleRemoveRecord() {
    if (!removeConfirm) return
    try {
      await getClient().removeDNSRecord(removeConfirm.name, removeConfirm.record_type)
      toast.success(t('dns.toast_record_removed'))
      setRemoveConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setRemoveConfirm(null)
    }
  }

  async function handleChangeTLD(e) {
    e.preventDefault()
    const tld = e.target.elements.tld.value.trim()
    try {
      await getClient().setDNSTLD(tld)
      toast.success(t('dns.toast_tld_changed'))
      setChangeTLDDialog(false)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleSetup() {
    try {
      await getClient().setupDNS()
      toast.success(t('dns.toast_setup_complete'))
      setSetupConfirm(false)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setSetupConfirm(false)
    }
  }

  const columns = [
    {
      key: 'name',
      label: t('dns.col_name'),
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'record_type',
      label: t('dns.col_type'),
      transform: (v) => (
        <Badge variant="outline">{RECORD_TYPES[v] || v}</Badge>
      ),
    },
    {
      key: 'value',
      label: t('dns.col_value'),
      transform: (v) => <span className="font-mono text-sm">{v}</span>,
    },
    {
      key: 'ttl',
      label: t('dns.col_ttl'),
    },
    ...(account?.admin ? [{
      key: '_actions',
      label: t('dns.col_actions'),
      sortable: false,
      transform: (_, row) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setRemoveConfirm(row)}
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      ),
    }] : []),
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('dns.title')}</h1>
          <p className="text-muted-foreground">{t('dns.description')}</p>
        </div>
        {account?.admin && (
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setSetupConfirm(true)}>
              {t('dns.setup_btn')}
            </Button>
            <Button onClick={() => setAddDialog(true)}>
              <Plus className="h-4 w-4 mr-1" />
              {t('dns.add_record_btn')}
            </Button>
          </div>
        )}
      </div>

      {/* Status Card */}
      {status && (
        <Card>
          <CardContent>
            <div className="flex items-center gap-4 flex-wrap">
              <Badge variant={status.enabled ? 'default' : 'secondary'}>
                {status.enabled ? t('dns.status_enabled') : t('dns.status_disabled')}
              </Badge>
              <Badge variant={status.running ? 'default' : 'secondary'}>
                {status.running ? t('dns.status_running') : t('dns.status_stopped')}
              </Badge>
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">{t('dns.status_tld')}:</span>
                <span className="font-mono text-sm font-medium">{status.tld || '—'}</span>
                {account?.admin && (
                  <Button variant="outline" size="sm" onClick={() => setChangeTLDDialog(true)}>
                    {t('dns.change_tld_btn')}
                  </Button>
                )}
              </div>
              <span className="text-sm text-muted-foreground">
                {t('dns.status_records', { count: status.record_count ?? 0, s: (status.record_count ?? 0) === 1 ? '' : 's' })}
              </span>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Disabled state */}
      {status && !status.enabled && (
        <div className="rounded-md border border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950 p-4 text-sm text-blue-800 dark:text-blue-200">
          {t('dns.disabled_message')}
        </div>
      )}

      {/* Records table */}
      {(!status || status.enabled) && (
        <>
          {recordsLoading && records.length === 0 && (
            <div className="text-center py-8 text-muted-foreground animate-pulse">{t('dns.loading')}</div>
          )}

          <DataTable
            data={records}
            columns={columns}
            entryKey="name"
          />
        </>
      )}

      {/* Add Record Dialog */}
      <Dialog open={addDialog} onOpenChange={(v) => { if (!v) { setAddDialog(false); setAddRecordType('') } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('dns.add_dialog_title')}</DialogTitle>
            <DialogDescription>{t('dns.add_dialog_description')}</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleAddRecord}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="add-record-name">{t('dns.name_label')}</Label>
                <Input id="add-record-name" name="record_name" required />
              </div>
              <div className="space-y-2">
                <Label htmlFor="add-record-type">{t('dns.type_label')}</Label>
                <Select value={addRecordType} onValueChange={setAddRecordType} required>
                  <SelectTrigger id="add-record-type">
                    <SelectValue placeholder={t('dns.type_placeholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {ADD_RECORD_TYPES.map((rt) => (
                      <SelectItem key={rt} value={rt}>{rt}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="add-record-value">{t('dns.value_label')}</Label>
                <Input id="add-record-value" name="record_value" required />
              </div>
              <div className="space-y-2">
                <Label htmlFor="add-record-ttl">{t('dns.ttl_label')}</Label>
                <Input id="add-record-ttl" name="record_ttl" type="number" defaultValue="300" />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => { setAddDialog(false); setAddRecordType('') }}>
                {t('dns.cancel_btn')}
              </Button>
              <Button type="submit" disabled={!addRecordType}>
                {t('dns.add_submit')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Change TLD Dialog */}
      <Dialog open={changeTLDDialog} onOpenChange={(v) => !v && setChangeTLDDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('dns.change_tld_dialog_title')}</DialogTitle>
            <DialogDescription>{t('dns.change_tld_dialog_description')}</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleChangeTLD}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="change-tld">{t('dns.tld_label')}</Label>
                <Input id="change-tld" name="tld" defaultValue={status?.tld || ''} required />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setChangeTLDDialog(false)}>
                {t('dns.cancel_btn')}
              </Button>
              <Button type="submit">{t('dns.change_tld_submit')}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Setup DNS Confirm */}
      <ConfirmDialog
        open={setupConfirm}
        title={t('dns.setup_dialog_title')}
        onConfirm={handleSetup}
        onCancel={() => setSetupConfirm(false)}
        confirmLabel={t('dns.setup_confirm_btn')}
      >
        {t('dns.setup_confirm_message')}
      </ConfirmDialog>

      {/* Remove Record Confirm */}
      <ConfirmDialog
        open={!!removeConfirm}
        title={t('dns.remove_dialog_title')}
        onConfirm={handleRemoveRecord}
        onCancel={() => setRemoveConfirm(null)}
        confirmLabel={t('dns.remove_confirm_btn')}
        variant="destructive"
      >
        {t('dns.remove_confirm_message', {
          name: removeConfirm?.name || '',
          type: RECORD_TYPES[removeConfirm?.record_type] || removeConfirm?.record_type || '',
        })}
      </ConfirmDialog>
    </div>
  )
}
