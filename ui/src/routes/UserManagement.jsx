import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Plus } from 'lucide-react'

export default function UserManagement() {
  useEffect(() => { document.title = 'Town OS - Users' }, [])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [statusConfirm, setStatusConfirm] = useState(null)
  const [adminConfirm, setAdminConfirm] = useState(null)
  const [error, setError] = useState(null)
  const [success, setSuccess] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('username')
  const [sortDirection, setSortDirection] = useState('asc')

  const [accounts, refresh, loading] = usePolling(
    () => getClient().listAccounts(sortKey, sortDirection),
    [],
    [refreshKey, sortKey, sortDirection],
  )

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleUpdate(e) {
    e.preventDefault()
    setError(null)
    setSuccess(null)
    const form = e.target.elements

    if (form.password.value && form.password.value.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    if (form.password.value && form.password.value !== form.password2.value) {
      setError('Passwords do not match')
      return
    }

    const fields = {}
    if (form.password.value) fields.password = form.password.value
    if (form.email.value) fields.email = form.email.value
    if (form.phone.value) fields.phone = form.phone.value
    if (form.real_name.value) fields.real_name = form.real_name.value

    try {
      await getClient().updateAccount(editDialog.username, fields)
      setSuccess(`User "${editDialog.username}" updated`)
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  async function handleStatusToggle() {
    setError(null)
    try {
      if (statusConfirm.disabled) {
        await getClient().enableAccount(statusConfirm.username)
        setSuccess(`User "${statusConfirm.username}" activated`)
      } else {
        await getClient().disableAccount(statusConfirm.username)
        setSuccess(`User "${statusConfirm.username}" deactivated`)
      }
      setStatusConfirm(null)
      doRefresh()
    } catch (err) {
      setError(err.message)
      setStatusConfirm(null)
    }
  }

  async function handleAdminToggle() {
    setError(null)
    try {
      const newAdmin = !adminConfirm.admin
      await getClient().updateAccount(adminConfirm.username, { admin: newAdmin })
      setSuccess(`User "${adminConfirm.username}" ${newAdmin ? 'promoted to admin' : 'demoted to user'}`)
      setAdminConfirm(null)
      doRefresh()
    } catch (err) {
      setError(err.message)
      setAdminConfirm(null)
    }
  }

  const columns = [
    { key: 'username', label: 'Username' },
    { key: 'real_name', label: 'Name', transform: (v) => v || '-' },
    { key: 'email', label: 'Email', transform: (v) => v || '-' },
    { key: 'phone', label: 'Phone', transform: (v) => v || '-' },
    {
      key: 'admin',
      label: 'Role',
      transform: (v, row) => (
        <Badge
          variant={v ? 'default' : 'secondary'}
          className="cursor-pointer select-none"
          onClick={() => setAdminConfirm(row)}
        >
          {v ? 'Admin' : 'User'}
        </Badge>
      ),
    },
    {
      key: 'disabled',
      label: 'Status',
      transform: (v, row) => (
        <Badge
          variant={v ? 'destructive' : 'outline'}
          className={`cursor-pointer select-none ${v ? 'opacity-70' : ''}`}
          onClick={() => setStatusConfirm(row)}
        >
          {v ? 'Disabled' : 'Active'}
        </Badge>
      ),
    },
    {
      key: '_edit',
      label: 'Edit',
      sortable: false,
      transform: (_, row) => (
        <Button
          variant="outline"
          size="sm"
          onClick={() => setEditDialog({ open: true, ...row })}
        >
          Edit
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Users</h1>
          <p className="text-muted-foreground">Manage user accounts</p>
        </div>
        <Button asChild>
          <Link to="/dashboard/users/create">
            <Plus className="h-4 w-4 mr-1" />
            Create User
          </Link>
        </Button>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {success && (
        <Alert>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      )}

      {loading && accounts.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <DataTable
        data={accounts}
        columns={columns}
        entryKey="username"
        page={page}
        setPage={setPage}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={(key, dir) => {
          setSortKey(key)
          setSortDirection(dir)
        }}
        onReset={() => {
          setSortKey('username')
          setSortDirection('asc')
        }}
      />

      <Dialog
        open={editDialog.open}
        onOpenChange={(v) => !v && setEditDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit User: {editDialog.username}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleUpdate}>
            <div className="space-y-4 py-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="password">New Password</Label>
                  <Input
                    id="password"
                    name="password"
                    type="password"
                    placeholder="Leave blank to keep"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="password2">Confirm Password</Label>
                  <Input
                    id="password2"
                    name="password2"
                    type="password"
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="real_name">Real Name</Label>
                <Input
                  id="real_name"
                  name="real_name"
                  defaultValue={editDialog.real_name || ''}
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="email">Email</Label>
                  <Input
                    id="email"
                    name="email"
                    type="email"
                    defaultValue={editDialog.email || ''}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="phone">Phone</Label>
                  <Input
                    id="phone"
                    name="phone"
                    defaultValue={editDialog.phone || ''}
                  />
                </div>
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setEditDialog({ open: false })}
              >
                Cancel
              </Button>
              <Button type="submit">Save Changes</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!statusConfirm}
        title={statusConfirm?.disabled ? 'Activate User' : 'Deactivate User'}
        onConfirm={handleStatusToggle}
        onCancel={() => setStatusConfirm(null)}
        confirmLabel={statusConfirm?.disabled ? 'Activate' : 'Deactivate'}
        variant={statusConfirm?.disabled ? 'default' : 'destructive'}
      >
        Are you sure you want to {statusConfirm?.disabled ? 'activate' : 'deactivate'} user{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {statusConfirm?.username}
        </code>
        ?
      </ConfirmDialog>

      <ConfirmDialog
        open={!!adminConfirm}
        title={adminConfirm?.admin ? 'Demote to User' : 'Promote to Admin'}
        onConfirm={handleAdminToggle}
        onCancel={() => setAdminConfirm(null)}
        confirmLabel={adminConfirm?.admin ? 'Demote' : 'Promote'}
        variant={adminConfirm?.admin ? 'destructive' : 'default'}
      >
        Are you sure you want to {adminConfirm?.admin ? 'demote' : 'promote'} user{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {adminConfirm?.username}
        </code>
        {adminConfirm?.admin ? ' to a regular user' : ' to admin'}?
      </ConfirmDialog>
    </div>
  )
}
