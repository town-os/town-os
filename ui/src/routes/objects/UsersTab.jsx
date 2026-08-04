import { useState } from 'react'
import { toast } from 'sonner'

import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { useI18n } from '@/i18n/I18nContext.jsx'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

/**
 * The users of one partition.
 *
 * Adding one projects a Town OS account into the partition's permission forest.
 * No password is involved: creating a principal needs none, and Town OS already
 * knows whether the account is an administrator — which is the only thing the
 * ceiling depends on.
 */
export default function UsersTab({ network, canManage, onChanged }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [addOpen, setAddOpen] = useState(false)
  const [selectedAccount, setSelectedAccount] = useState('')
  const [removeTarget, setRemoveTarget] = useState(null)
  const [busy, setBusy] = useState(false)

  const [principals] = usePolling(
    () => (network ? getClient().listGfehPrincipals(network).catch(() => []) : Promise.resolve([])),
    [],
    [network, refreshKey],
    30000,
  )

  // A normal interval, not 0: usePolling passes it straight to setInterval, so
  // zero means "poll as fast as the event loop allows" rather than "once".
  //
  // `entries` is the key every paginated Town OS list answers with. Narrowing
  // to an array rather than falling back to the response itself is deliberate:
  // an envelope reaching `candidates` as a non-array throws out of render and
  // white-screens the tab, which is a far worse failure than showing nobody.
  const [accounts] = usePolling(
    () => getClient().listAccounts().then((r) => (Array.isArray(r?.entries) ? r.entries : [])).catch(() => []),
    [],
    [refreshKey],
    60000,
  )

  // Only accounts not already here, so the dialog cannot offer a duplicate the
  // server would reject with a conflict.
  const candidates = (accounts || []).filter(
    (a) => !principals.some((p) => p.name === a.username),
  )

  function refresh() {
    setRefreshKey((k) => k + 1)
    if (onChanged) onChanged()
  }

  async function handleAdd(e) {
    e.preventDefault()
    if (!selectedAccount) return
    setBusy(true)
    try {
      await getClient().addGfehPrincipal(network, selectedAccount)
      toast.success(t('objects.toast_user_added'))
      setAddOpen(false)
      setSelectedAccount('')
      refresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setBusy(false)
    }
  }

  async function handleRemove() {
    setBusy(true)
    try {
      await getClient().removeGfehPrincipal(network, removeTarget.name)
      toast.success(t('objects.toast_user_removed'))
      setRemoveTarget(null)
      refresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setBusy(false)
    }
  }

  const columns = [
    { key: 'name', label: t('objects.col_user'), sortable: true },
    {
      key: 'account',
      label: t('objects.col_kind'),
      transform: (v) => (
        <Badge variant={v ? 'default' : 'secondary'}>
          {v ? t('objects.kind_account') : t('objects.kind_subuser')}
        </Badge>
      ),
    },
    {
      key: 'ceiling',
      label: t('objects.col_ceiling'),
      transform: (v) => <code className="font-mono text-xs">{(v || []).join(', ')}</code>,
    },
    {
      key: 'actions',
      label: '',
      transform: (_v, row) =>
        canManage ? (
          <Button variant="ghost" size="sm" onClick={() => setRemoveTarget(row)}>
            {t('objects.remove_user')}
          </Button>
        ) : null,
    },
  ]

  return (
    <div className="space-y-4">
      {canManage && (
        <div className="flex justify-end">
          <Button onClick={() => setAddOpen(true)} disabled={candidates.length === 0}>
            {t('objects.add_user')}
          </Button>
        </div>
      )}

      {principals.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('objects.no_users')}</p>
      ) : (
        <DataTable data={principals} columns={columns} entryKey="name" />
      )}

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <form onSubmit={handleAdd}>
            <DialogHeader>
              <DialogTitle>{t('objects.add_user_title')}</DialogTitle>
              <DialogDescription>{t('objects.add_user_description')}</DialogDescription>
            </DialogHeader>

            <div className="space-y-2 py-4">
              <Label htmlFor="gfeh-account">{t('objects.field_account')}</Label>
              <select
                id="gfeh-account"
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
                value={selectedAccount}
                onChange={(e) => setSelectedAccount(e.target.value)}
              >
                <option value="">{t('objects.select_user')}</option>
                {candidates.map((a) => (
                  <option key={a.username} value={a.username}>
                    {a.username}
                  </option>
                ))}
              </select>
            </div>

            <DialogFooter>
              <Button type="submit" disabled={busy || !selectedAccount}>
                {t('objects.add_user')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!removeTarget}
        title={t('objects.remove_user')}
        confirmLabel={t('objects.remove_user')}
        loading={busy}
        onConfirm={handleRemove}
        onCancel={() => setRemoveTarget(null)}
      >
        {t('objects.remove_user_confirm')}
      </ConfirmDialog>
    </div>
  )
}
