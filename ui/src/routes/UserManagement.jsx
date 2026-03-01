import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
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
  useEffect(() => { document.title = 'Town OS - Users' }, [])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [statusConfirm, setStatusConfirm] = useState(null)
  const [adminCount, setAdminCount] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('username')
  const [sortDirection, setSortDirection] = useState('asc')
  const [search, setSearch] = useState('')

  const [accountData, refresh, loading] = usePolling(
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
      toast.error('Password must be at least 8 characters')
      return
    }
    if (form.password.value && form.password.value !== form.password2.value) {
      toast.error('Passwords do not match')
      return
    }

    const fields = {}
    if (form.password.value) fields.password = form.password.value
    if (form.email.value) fields.email = form.email.value
    if (form.phone.value) fields.phone = form.phone.value
    if (form.real_name.value) fields.real_name = form.real_name.value

    try {
      await getClient().updateAccount(editDialog.username, fields)
      toast.success(`User "${editDialog.username}" updated`)
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleStatusToggle() {
    try {
      if (statusConfirm.disabled) {
        await getClient().enableAccount(statusConfirm.username)
        toast.success(`User "${statusConfirm.username}" activated`)
      } else {
        await getClient().disableAccount(statusConfirm.username)
        toast.success(`User "${statusConfirm.username}" deactivated`)
      }
      setStatusConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setStatusConfirm(null)
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
      transform: (v) => (
        <Badge variant={v ? 'default' : 'secondary'}>
          {v ? 'Admin' : 'User'}
        </Badge>
      ),
    },
    {
      key: 'disabled',
      label: 'Status',
      transform: (v, row) => (
        <Tooltip>
          <TooltipTrigger>
            <Badge
              variant={v ? 'destructive' : 'outline'}
              className={`cursor-pointer select-none ${v ? 'opacity-70' : ''}`}
              onClick={() => setStatusConfirm(row)}
            >
              {v ? 'Disabled' : 'Active'}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="right">
            Click to {v ? 'activate' : 'deactivate'}
          </TooltipContent>
        </Tooltip>
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

      {loading && accounts.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
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
            <DialogTitle>Edit User: {editDialog.username}</DialogTitle>
            <DialogDescription>Update account details for this user.</DialogDescription>
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
        {!statusConfirm?.disabled && statusConfirm?.admin && adminCount <= 1 ? (
          <>
            <strong>Warning:</strong> This is the last enabled admin account.
            Deactivating it will lock all users out of the system until a new
            admin account is created through the bootstrap flow.
          </>
        ) : (
          <>
            Are you sure you want to {statusConfirm?.disabled ? 'activate' : 'deactivate'} user{' '}
            <code className="font-mono text-sm bg-muted px-1 rounded">
              {statusConfirm?.username}
            </code>
            ?
          </>
        )}
      </ConfirmDialog>
    </div>
  )
}
