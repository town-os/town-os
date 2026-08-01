import { useState } from 'react'
import { toast } from 'sonner'

import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { useI18n } from '@/i18n/I18nContext.jsx'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
 * The permissions gfeh understands, in the order they are worth offering.
 *
 * Listed rather than fetched: they are a property of the protocol, and a
 * dropdown that had to wait on a round trip before an admin could grant
 * anything would be worse for no gain.
 */
const PERMISSIONS = [
  'read',
  'list',
  'create',
  'write',
  'delete',
  'meta-read',
  'meta-write',
  'share',
  'admin-acl',
  'create-subuser',
  'read-audit',
  'federate',
  'publish-http',
  'publish-ipfs',
  'snapshot',
  'quota',
]

/**
 * One user's grants in one partition.
 *
 * A principal has to be selected first, because gfehd's grant listing requires
 * one — an absent principal is an error there rather than "every grant", and
 * pretending otherwise here would make the UI disagree with what it talks to.
 */
export default function GrantsTab({ network, isAdmin }) {
  const { t } = useI18n()
  const [refreshKey, setRefreshKey] = useState(0)
  const [principal, setPrincipal] = useState('')
  const [addOpen, setAddOpen] = useState(false)
  const [perms, setPerms] = useState([])
  const [revokeTarget, setRevokeTarget] = useState(null)
  const [busy, setBusy] = useState(false)

  const [principals] = usePolling(
    () => (network ? getClient().listGfehPrincipals(network).catch(() => []) : Promise.resolve([])),
    [],
    [network, refreshKey],
    30000,
  )

  const [grants] = usePolling(
    () =>
      network && principal
        ? getClient().listGfehGrants(network, principal).catch(() => [])
        : Promise.resolve([]),
    [],
    [network, principal, refreshKey],
    30000,
  )

  function togglePerm(perm) {
    setPerms((current) =>
      current.includes(perm) ? current.filter((p) => p !== perm) : [...current, perm],
    )
  }

  async function handleAdd(e) {
    e.preventDefault()
    const path = e.target.elements.path.value.trim()
    const inheritable = e.target.elements.inheritable.checked
    if (!path || perms.length === 0) return

    setBusy(true)
    try {
      const granted = await getClient().addGfehGrant(network, principal, path, perms, inheritable)
      // Compared against what was asked for: gfeh clamps a grant to the
      // principal's ceiling, and an admin who is not told would believe they
      // gave access nobody has.
      if (granted.perm.length !== perms.length) {
        toast.warning(t('objects.grant_clamped'))
      } else {
        toast.success(t('objects.toast_grant_added'))
      }
      setAddOpen(false)
      setPerms([])
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setBusy(false)
    }
  }

  async function handleRevoke() {
    setBusy(true)
    try {
      await getClient().revokeGfehGrant(network, revokeTarget.id)
      toast.success(t('objects.toast_grant_revoked'))
      setRevokeTarget(null)
      setRefreshKey((k) => k + 1)
    } catch (err) {
      toast.error(err.detail || err.message)
    } finally {
      setBusy(false)
    }
  }

  const columns = [
    {
      key: 'path',
      label: t('objects.col_path'),
      sortable: true,
      transform: (v) => <code className="font-mono text-xs">{v}</code>,
    },
    {
      key: 'perm',
      label: t('objects.col_perm'),
      transform: (v) => <code className="font-mono text-xs">{(v || []).join(', ')}</code>,
    },
    {
      key: 'inheritable',
      label: t('objects.col_inheritable'),
      transform: (v) => (v ? '✓' : ''),
    },
    {
      key: 'actions',
      label: '',
      transform: (_v, row) =>
        isAdmin ? (
          <Button variant="ghost" size="sm" onClick={() => setRevokeTarget(row)}>
            {t('objects.revoke_grant')}
          </Button>
        ) : null,
    },
  ]

  return (
    <div className="space-y-4">
      <div className="flex items-end justify-between gap-4">
        <div className="space-y-2">
          <Label htmlFor="grant-principal">{t('objects.col_user')}</Label>
          <select
            id="grant-principal"
            className="w-56 rounded-md border bg-background px-3 py-2 text-sm"
            value={principal}
            onChange={(e) => setPrincipal(e.target.value)}
          >
            <option value="">{t('objects.select_user')}</option>
            {principals.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
        {isAdmin && (
          <Button onClick={() => setAddOpen(true)} disabled={!principal}>
            {t('objects.add_grant')}
          </Button>
        )}
      </div>

      {!principal ? null : grants.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('objects.no_grants')}</p>
      ) : (
        <DataTable data={grants} columns={columns} entryKey="id" />
      )}

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <form onSubmit={handleAdd}>
            <DialogHeader>
              <DialogTitle>{t('objects.add_grant_title')}</DialogTitle>
              <DialogDescription>{t('objects.add_grant_description')}</DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="grant-path">{t('objects.field_path')}</Label>
                <Input id="grant-path" name="path" defaultValue="/" required />
              </div>

              <div className="space-y-2">
                <Label>{t('objects.field_perm')}</Label>
                <div className="grid grid-cols-3 gap-2">
                  {PERMISSIONS.map((perm) => (
                    <label key={perm} className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={perms.includes(perm)}
                        onChange={() => togglePerm(perm)}
                      />
                      <span className="font-mono text-xs">{perm}</span>
                    </label>
                  ))}
                </div>
              </div>

              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" name="inheritable" defaultChecked />
                {t('objects.field_inheritable')}
              </label>
            </div>

            <DialogFooter>
              <Button type="submit" disabled={busy || perms.length === 0}>
                {t('objects.add_grant')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!revokeTarget}
        title={t('objects.revoke_grant')}
        confirmLabel={t('objects.revoke_grant')}
        loading={busy}
        onConfirm={handleRevoke}
        onCancel={() => setRevokeTarget(null)}
      >
        {t('objects.revoke_grant_confirm')}
      </ConfirmDialog>
    </div>
  )
}
