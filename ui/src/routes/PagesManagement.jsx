import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Plus, Trash2, Pencil, RefreshCw, Globe } from 'lucide-react'

function statusBadge(status) {
  if (status === 'active') return <Badge className="bg-green-600 text-white hover:bg-green-600/90">active</Badge>
  if (status === 'error') return <Badge className="bg-red-600 text-white hover:bg-red-600/90">error</Badge>
  return <Badge className="bg-gray-500 text-white hover:bg-gray-500/90">pending</Badge>
}

export default function PagesManagement() {
  useEffect(() => { document.title = 'Town OS - Pages' }, [])

  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('name')
  const [sortDirection, setSortDirection] = useState('asc')
  const [search, setSearch] = useState('')

  const [createDialog, setCreateDialog] = useState({ open: false })
  const [editDialog, setEditDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [rebuildConfirm, setRebuildConfirm] = useState(null)

  const PAGE_SIZE = 20

  const [data, , loading] = usePolling(
    () => getClient().listPages(sortKey, sortDirection, PAGE_SIZE, page * PAGE_SIZE, search || undefined),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey, sortKey, sortDirection, page, search],
  )
  const pages = data.entries || []

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleCreate(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      const name = form.name.value.trim()
      const repoURL = form.repoURL.value.trim()
      const branch = form.branch.value.trim() || 'main'
      const domain = form.domain.value.trim() || name
      await getClient().createPage(name, repoURL, branch, domain)
      toast.success(`Page "${name}" created`)
      setCreateDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleEdit(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      await getClient().updatePage(editDialog.name, {
        repo_url: form.repoURL.value.trim(),
        branch: form.branch.value.trim() || 'main',
        domain: form.domain.value.trim() || editDialog.name,
      })
      toast.success(`Page "${editDialog.name}" updated`)
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleDelete() {
    try {
      await getClient().removePage(deleteConfirm)
      toast.success(`Page "${deleteConfirm}" removed`)
      setDeleteConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setDeleteConfirm(null)
    }
  }

  async function handleRebuild() {
    try {
      await getClient().rebuildPage(rebuildConfirm)
      toast.success(`Rebuild triggered for "${rebuildConfirm}"`)
      setRebuildConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.message)
      setRebuildConfirm(null)
    }
  }

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'domain', label: 'Domain' },
    { key: 'repo_url', label: 'Repo URL' },
    { key: 'branch', label: 'Branch' },
    {
      key: 'status',
      label: 'Status',
      transform: (v) => statusBadge(v),
    },
    {
      key: '_actions',
      label: 'Actions',
      sortable: false,
      transform: (_, row) => (
        <div className="flex items-center justify-end gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            title="Rebuild"
            onClick={() => setRebuildConfirm(row.name)}
          >
            <RefreshCw className="h-3 w-3" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            title="Edit"
            onClick={() =>
              setEditDialog({
                open: true,
                name: row.name,
                repoURL: row.repo_url,
                branch: row.branch,
                domain: row.domain,
              })
            }
          >
            <Pencil className="h-3 w-3" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-destructive hover:text-destructive"
            title="Delete"
            onClick={() => setDeleteConfirm(row.name)}
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Pages</h1>
          <p className="text-muted-foreground">
            Manage static site hosting
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={() => setCreateDialog({ open: true })}>
            <Plus className="h-4 w-4 mr-1" />
            Create Page
          </Button>
        </div>
      </div>

      {loading && pages.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <Globe className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-lg font-semibold">Sites</h3>
          <span className="text-sm text-muted-foreground ml-auto">
            {pages.length} page{pages.length !== 1 ? 's' : ''}
          </span>
        </div>
        <DataTable
          data={pages}
          columns={columns}
          entryKey="name"
          page={page}
          setPage={setPage}
          pageSize={PAGE_SIZE}
          hasMore={data.has_more}
          totalPages={data.total_pages}
          totalCount={data.total_count}
          sortKey={sortKey}
          sortDirection={sortDirection}
          onSortChange={(key, dir) => {
            setSortKey(key)
            setSortDirection(dir)
            setPage(0)
          }}
          onReset={() => {
            setSortKey('name')
            setSortDirection('asc')
            setSearch('')
            setPage(0)
          }}
          onSearchChange={(s) => {
            setSearch(s)
            setPage(0)
          }}
        />
      </div>

      {/* Create Page dialog */}
      <Dialog
        open={createDialog.open}
        onOpenChange={(v) => !v && setCreateDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Globe className="h-4 w-4 inline mr-2" />
              Create Page
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleCreate}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  name="name"
                  required
                  placeholder="my-site"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="repoURL">Repo URL</Label>
                <Input
                  id="repoURL"
                  name="repoURL"
                  required
                  placeholder="https://github.com/user/repo.git"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="branch">Branch</Label>
                <Input
                  id="branch"
                  name="branch"
                  placeholder="main"
                />
                <p className="text-xs text-muted-foreground">Defaults to &quot;main&quot; if left empty</p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="domain">Domain</Label>
                <Input
                  id="domain"
                  name="domain"
                  placeholder="my-site.example.com"
                />
                <p className="text-xs text-muted-foreground">Defaults to the page name if left empty</p>
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setCreateDialog({ open: false })}
              >
                Cancel
              </Button>
              <Button type="submit">Create</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit Page dialog */}
      <Dialog
        open={editDialog.open}
        onOpenChange={(v) => !v && setEditDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Pencil className="h-4 w-4 inline mr-2" />
              Edit Page &mdash; {editDialog.name}
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleEdit}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="editRepoURL">Repo URL</Label>
                <Input
                  id="editRepoURL"
                  name="repoURL"
                  required
                  defaultValue={editDialog.repoURL || ''}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="editBranch">Branch</Label>
                <Input
                  id="editBranch"
                  name="branch"
                  defaultValue={editDialog.branch || ''}
                  placeholder="main"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="editDomain">Domain</Label>
                <Input
                  id="editDomain"
                  name="domain"
                  defaultValue={editDialog.domain || ''}
                  placeholder={editDialog.name || ''}
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
              <Button type="submit">Save Changes</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteConfirm}
        title="Delete Page"
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
        confirmLabel="Delete"
        variant="destructive"
      >
        Are you sure you want to delete page{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {deleteConfirm}
        </code>
        ? This cannot be undone.
      </ConfirmDialog>

      {/* Rebuild confirmation */}
      <ConfirmDialog
        open={!!rebuildConfirm}
        title="Rebuild Page"
        onConfirm={handleRebuild}
        onCancel={() => setRebuildConfirm(null)}
        confirmLabel="Rebuild"
      >
        Trigger a rebuild for page{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">
          {rebuildConfirm}
        </code>
        ? This will pull the latest source and redeploy.
      </ConfirmDialog>
    </div>
  )
}
