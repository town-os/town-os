import { useState, useEffect, useCallback } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2, Pencil, HardDrive } from 'lucide-react'

const UNITS = {
  B: 1,
  MB: 1024 * 1024,
  GB: 1024 * 1024 * 1024,
  TB: 1024 * 1024 * 1024 * 1024,
}

function formatQuotaText(bytes) {
  if (!bytes || bytes === 0) return 'none'
  if (bytes >= UNITS.TB) return `${(bytes / UNITS.TB).toFixed(2)} TB`
  if (bytes >= UNITS.GB) return `${(bytes / UNITS.GB).toFixed(2)} GB`
  if (bytes >= UNITS.MB) return `${(bytes / UNITS.MB).toFixed(2)} MB`
  return `${bytes} B`
}

function formatQuota(bytes) {
  if (!bytes || bytes === 0) return <Badge className="bg-black text-white hover:bg-black/90">none</Badge>
  return formatQuotaText(bytes)
}

/** Decompose a byte count into a [value, unit] pair for the form. */
function decomposeQuota(bytes) {
  if (!bytes || bytes === 0) return ['', 'GB']
  if (bytes >= UNITS.TB && bytes % UNITS.TB === 0) return [bytes / UNITS.TB, 'TB']
  if (bytes >= UNITS.GB && bytes % UNITS.GB === 0) return [bytes / UNITS.GB, 'GB']
  if (bytes >= UNITS.MB && bytes % UNITS.MB === 0) return [bytes / UNITS.MB, 'MB']
  return [bytes, 'B']
}

export default function StorageManagement() {
  useEffect(() => { document.title = 'Town OS - Storage' }, [])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
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

  function parseQuotaFromForm(form) {
    const raw = form.quota.value ? parseFloat(form.quota.value) : 0
    if (raw === 0) return 0
    const unit = form.quotaUnit.value
    return Math.round(raw * (UNITS[unit] || 1))
  }

  async function handleCreate(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      const quota = parseQuotaFromForm(form)
      await getClient().createFilesystem({
        name: form.name.value,
        quota,
      })
      toast.success(quota > 0
        ? `Filesystem created with ${formatQuotaText(quota)} quota`
        : 'Filesystem created')
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleModify(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      const quota = parseQuotaFromForm(form)
      await getClient().modifyFilesystem(editDialog.originalName, {
        name: form.name.value,
        quota,
      })
      toast.success(quota > 0
        ? `Filesystem modified with ${formatQuotaText(quota)} quota`
        : 'Filesystem modified')
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleDelete() {
    try {
      await getClient().removeFilesystem(deleteConfirm)
      toast.success(`Filesystem "${deleteConfirm}" removed`)
      setDeleteConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
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
      className: 'text-center',
      transform: (_, row) => (
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            const [qv, qu] = decomposeQuota(row.quota)
            setEditDialog({
              open: true,
              create: false,
              originalName: row.name,
              name: row.name,
              quotaValue: qv,
              quotaUnit: qu,
            })}
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
            setEditDialog({ open: true, create: true, name: '', quotaValue: '', quotaUnit: 'GB' })
          }
        >
          <Plus className="h-4 w-4 mr-1" />
          Create Filesystem
        </Button>
      </div>

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
                <Label htmlFor="quota">Quota (0 = unlimited)</Label>
                <div className="flex gap-2">
                  <Input
                    id="quota"
                    name="quota"
                    type="number"
                    min="0"
                    step="any"
                    defaultValue={editDialog.quotaValue || ''}
                    placeholder="0"
                    className="flex-1"
                  />
                  <select
                    name="quotaUnit"
                    defaultValue={editDialog.quotaUnit || 'GB'}
                    className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                  >
                    <option value="B">B</option>
                    <option value="MB">MB</option>
                    <option value="GB">GB</option>
                    <option value="TB">TB</option>
                  </select>
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
