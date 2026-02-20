import { useState, useEffect, useCallback } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Plus, Trash2, Pencil, HardDrive } from 'lucide-react'

function formatQuota(bytes) {
  if (!bytes || bytes === 0) return 'none'
  if (bytes >= 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
  }
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
  }
  return `${bytes} B`
}

export default function StorageManagement() {
  useEffect(() => { document.title = 'Town OS - Storage' }, [])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [error, setError] = useState(null)
  const [success, setSuccess] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('name')
  const [sortDirection, setSortDirection] = useState('asc')

  const [filesystems, refresh, loading] = usePolling(
    () => getClient().listFilesystems('', sortKey, sortDirection),
    [],
    [refreshKey, sortKey, sortDirection],
  )

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleCreate(e) {
    e.preventDefault()
    setError(null)
    setSuccess(null)
    const form = e.target.elements
    try {
      await getClient().createFilesystem({
        name: form.name.value,
        quota: form.quota.value ? parseInt(form.quota.value, 10) : 0,
      })
      setSuccess('Filesystem created')
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  async function handleModify(e) {
    e.preventDefault()
    setError(null)
    setSuccess(null)
    const form = e.target.elements
    try {
      await getClient().modifyFilesystem(editDialog.originalName, {
        name: form.name.value,
        quota: form.quota.value ? parseInt(form.quota.value, 10) : 0,
      })
      setSuccess('Filesystem modified')
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  async function handleDelete() {
    setError(null)
    setSuccess(null)
    try {
      await getClient().removeFilesystem(deleteConfirm)
      setSuccess(`Filesystem "${deleteConfirm}" removed`)
      setDeleteConfirm(null)
      doRefresh()
    } catch (err) {
      setError(err.message)
      setDeleteConfirm(null)
    }
  }

  const columns = [
    { key: 'name', label: 'Name' },
    {
      key: 'quota',
      label: 'Quota',
      transform: (v) => formatQuota(v),
    },
    {
      key: '_modify',
      label: 'Modify',
      sortable: false,
      transform: (_, row) => (
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            setEditDialog({
              open: true,
              create: false,
              originalName: row.name,
              name: row.name,
              quota: row.quota,
            })
          }
        >
          <Pencil className="h-3 w-3 mr-1" />
          Modify
        </Button>
      ),
    },
    {
      key: '_delete',
      label: 'Delete',
      sortable: false,
      transform: (_, row) => (
        <Button
          variant="ghost"
          size="sm"
          className="text-destructive hover:text-destructive"
          onClick={() => setDeleteConfirm(row.name)}
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Storage</h1>
          <p className="text-muted-foreground">
            Manage btrfs subvolumes
          </p>
        </div>
        <Button
          onClick={() =>
            setEditDialog({ open: true, create: true, name: '', quota: '' })
          }
        >
          <Plus className="h-4 w-4 mr-1" />
          Create Filesystem
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

      {loading && filesystems.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <DataTable
        data={filesystems}
        columns={columns}
        entryKey="name"
        page={page}
        setPage={setPage}
        sortKey={sortKey}
        sortDirection={sortDirection}
        onSortChange={(key, dir) => {
          setSortKey(key)
          setSortDirection(dir)
        }}
        onReset={() => {
          setSortKey('name')
          setSortDirection('asc')
        }}
      />

      <Dialog
        open={editDialog.open}
        onOpenChange={(v) => !v && setEditDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <HardDrive className="h-4 w-4 inline mr-2" />
              {editDialog.create ? 'Create' : 'Modify'} Filesystem
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={editDialog.create ? handleCreate : handleModify}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  name="name"
                  defaultValue={editDialog.name || ''}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="quota">Quota (bytes, 0 = unlimited)</Label>
                <Input
                  id="quota"
                  name="quota"
                  type="number"
                  min="0"
                  defaultValue={editDialog.quota || ''}
                  placeholder="0"
                />
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
              <Button type="submit">
                {editDialog.create ? 'Create' : 'Save Changes'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!deleteConfirm}
        title="Delete Filesystem"
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
        confirmLabel="Delete"
        variant="destructive"
      >
        Are you sure you want to delete filesystem{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {deleteConfirm}
        </code>
        ? This cannot be undone.
      </ConfirmDialog>
    </div>
  )
}
