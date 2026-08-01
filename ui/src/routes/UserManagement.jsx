import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from '@/components/ui/tooltip'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Plus } from 'lucide-react'

export default function UserManagement() {
  const { t } = useI18n()
  useEffect(() => { document.title = t('users.page_title') }, [t])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [editWireguard, setEditWireguard] = useState(false)
  const [editNetworks, setEditNetworks] = useState([])
  const [networks, setNetworks] = useState([])
  const [statusConfirm, setStatusConfirm] = useState(null)
  const [adminCount, setAdminCount] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('username')
  const [sortDirection, setSortDirection] = useState('asc')
  const [search, setSearch] = useState('')

  // The default "home" network has no WireGuard transport, so it is never a
  // valid scope for a WireGuard-only account.
  useEffect(() => {
    getClient().listNetworks()
      .then((nets) => setNetworks((nets || []).filter((n) => n.name !== 'home')))
      .catch((err) => console.debug('listNetworks failed:', err))
  }, [])

  function openEdit(row) {
    setEditWireguard(!!row.wireguard)
    setEditNetworks(row.networks || [])
    setEditDialog({ open: true, ...row })
  }

  function toggleEditNetwork(name) {
    setEditNetworks((prev) =>
      prev.includes(name) ? prev.filter((n) => n !== name) : [...prev, name],
    )
  }

  const [accountData, , loading] = usePolling(
    () => getClient().listAccounts(sortKey, sortDirection, PAGE_SIZE, page * PAGE_SIZE, search || undefined),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey, sortKey, sortDirection, page, search],
  )
  const accounts = accountData.entries || []

  useEffect(() => {
    getClient().ping().then((r) => setAdminCount(r.admins)).catch((err) => console.debug('ping failed:', err))
  }, [refreshKey])

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleUpdate(e) {
    e.preventDefault()
    const form = e.target.elements

    if (form.password.value && form.password.value.length < 8) {
      toast.error(t('users.error_password_min_length'))
      return
    }
    if (form.password.value && form.password.value !== form.password2.value) {
      toast.error(t('users.error_passwords_mismatch'))
      return
    }

    if (editWireguard && editNetworks.length === 0) {
      toast.error(t('users.error_networks_required'))
      return
    }

    const fields = {}
    if (form.password.value) fields.password = form.password.value
    if (form.email.value) fields.email = form.email.value
    if (form.phone.value) fields.phone = form.phone.value
    if (form.real_name.value) fields.real_name = form.real_name.value
    // The dialog is authoritative over the WireGuard restriction and its scope.
    // admin is immutable and is never sent.
    fields.wireguard = editWireguard
    fields.networks = editWireguard ? editNetworks : []

    // Three states, and they have to stay distinguishable: an untouched field
    // leaves the credential alone, a password sets one, and the clear checkbox
    // revokes it. Sending the empty string for an untouched field would
    // silently withdraw SMB access on every unrelated edit.
    if (form.smb_password.value) {
      fields.smb_password = form.smb_password.value
    } else if (form.smb_clear && form.smb_clear.checked) {
      fields.smb_password = ''
    }

    try {
      await getClient().updateAccount(editDialog.username, fields)
      toast.success(t('users.toast_updated'))
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleStatusToggle() {
    try {
      if (statusConfirm.disabled) {
        await getClient().enableAccount(statusConfirm.username)
        toast.success(t('users.toast_activated'))
      } else {
        await getClient().disableAccount(statusConfirm.username)
        toast.success(t('users.toast_deactivated'))
      }
      setStatusConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setStatusConfirm(null)
    }
  }

  const columns = [
    { key: 'username', label: t('users.col_username') },
    { key: 'real_name', label: t('users.col_name'), transform: (v) => v || '-' },
    { key: 'email', label: t('users.col_email'), transform: (v) => v || '-' },
    { key: 'phone', label: t('users.col_phone'), transform: (v) => v || '-' },
    {
      key: 'admin',
      label: t('users.col_role'),
      transform: (v, row) => {
        if (row.wireguard) {
          return <Badge variant="outline">{t('users.role_wireguard')}</Badge>
        }
        return (
          <Badge variant={v ? 'default' : 'secondary'}>
            {v ? t('users.role_admin') : t('users.role_user')}
          </Badge>
        )
      },
    },
    {
      key: 'disabled',
      label: t('users.col_status'),
      transform: (v, row) => (
        <Tooltip>
          <TooltipTrigger>
            <Badge
              variant={v ? 'destructive' : 'outline'}
              className={`cursor-pointer select-none ${v ? 'opacity-70' : ''}`}
              onClick={() => setStatusConfirm(row)}
            >
              {v ? t('users.status_disabled') : t('users.status_active')}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="right">
            {v ? t('users.tooltip_activate') : t('users.tooltip_deactivate')}
          </TooltipContent>
        </Tooltip>
      ),
    },
    {
      key: '_edit',
      label: t('users.col_edit'),
      sortable: false,
      transform: (_, row) => (
        <Button
          variant="outline"
          size="sm"
          onClick={() => openEdit(row)}
        >
          {t('users.edit_btn')}
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('users.title')}</h1>
          <p className="text-muted-foreground">{t('users.description')}</p>
        </div>
        <Button asChild>
          <Link to="/dashboard/users/create">
            <Plus className="h-4 w-4 mr-1" />
            {t('users.create_btn')}
          </Link>
        </Button>
      </div>

      {loading && accounts.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('users.loading')}</div>
      )}

      <DataTable
        data={accounts}
        columns={columns}
        entryKey="username"
        page={page}
        setPage={setPage}
        pageSize={PAGE_SIZE}
        hasMore={accountData.has_more}
        totalPages={accountData.total_pages}
        totalCount={accountData.total_count}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={(key, dir) => {
          setSortKey(key)
          setSortDirection(dir)
          setPage(0)
        }}
        onReset={() => {
          setSortKey('username')
          setSortDirection('asc')
          setSearch('')
          setPage(0)
        }}
        onSearchChange={(s) => {
          setSearch(s)
          setPage(0)
        }}
      />

      <Dialog
        open={editDialog.open}
        onOpenChange={(v) => !v && setEditDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('users.edit_dialog_title')}: {editDialog.username}</DialogTitle>
            <DialogDescription>{t('users.edit_dialog_description')}</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleUpdate}>
            <div className="space-y-4 py-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="password">{t('users.new_password_label')}</Label>
                  <Input
                    id="password"
                    name="password"
                    type="password"
                    placeholder={t('users.password_placeholder')}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="password2">{t('users.confirm_password_label')}</Label>
                  <Input
                    id="password2"
                    name="password2"
                    type="password"
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="real_name">{t('users.real_name_label')}</Label>
                <Input
                  id="real_name"
                  name="real_name"
                  defaultValue={editDialog.real_name || ''}
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="email">{t('users.email_label')}</Label>
                  <Input
                    id="email"
                    name="email"
                    type="email"
                    defaultValue={editDialog.email || ''}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="phone">{t('users.phone_label')}</Label>
                  <Input
                    id="phone"
                    name="phone"
                    defaultValue={editDialog.phone || ''}
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="smb_password">{t('users.smb_credential')}</Label>
                <Input
                  id="smb_password"
                  name="smb_password"
                  type="password"
                  autoComplete="new-password"
                />
                <p className="text-xs text-muted-foreground">
                  {t('users.smb_credential_help')}
                </p>
                {editDialog.smb_enrolled ? (
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" id="smb_clear" name="smb_clear" className="rounded" />
                    {t('users.smb_clear')}
                  </label>
                ) : (
                  <p className="text-xs text-muted-foreground">{t('users.smb_not_enrolled')}</p>
                )}
              </div>
              {!editDialog.admin && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="edit-wireguard"
                      className="rounded"
                      checked={editWireguard}
                      onChange={(e) => setEditWireguard(e.target.checked)}
                    />
                    <Label htmlFor="edit-wireguard">{t('users.wireguard_label')}</Label>
                  </div>
                  {editWireguard && (
                    <div className="space-y-1">
                      <Label>{t('users.networks_label')}</Label>
                      {networks.length === 0 ? (
                        <p className="text-sm text-muted-foreground">{t('users.networks_none')}</p>
                      ) : (
                        networks.map((n) => (
                          <div key={n.name} className="flex items-center gap-2">
                            <input
                              type="checkbox"
                              id={`edit-network-${n.name}`}
                              className="rounded"
                              checked={editNetworks.includes(n.name)}
                              onChange={() => toggleEditNetwork(n.name)}
                            />
                            <Label htmlFor={`edit-network-${n.name}`}>{n.name}</Label>
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setEditDialog({ open: false })}
              >
                {t('users.cancel_btn')}
              </Button>
              <Button type="submit">{t('users.save_changes')}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!statusConfirm}
        title={statusConfirm?.disabled ? t('users.activate_dialog_title') : t('users.deactivate_dialog_title')}
        onConfirm={handleStatusToggle}
        onCancel={() => setStatusConfirm(null)}
        confirmLabel={statusConfirm?.disabled ? t('users.activate_confirm_btn') : t('users.deactivate_confirm_btn')}
        variant={statusConfirm?.disabled ? 'default' : 'destructive'}
      >
        {!statusConfirm?.disabled && statusConfirm?.admin && adminCount <= 1 ? (
          t('users.deactivate_last_admin_warning')
        ) : (
          statusConfirm?.disabled
            ? t('users.activate_confirm_message', { username: statusConfirm?.username })
            : t('users.deactivate_confirm_message', { username: statusConfirm?.username })
        )}
      </ConfirmDialog>
    </div>
  )
}
