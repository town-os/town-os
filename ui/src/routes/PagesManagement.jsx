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
import { Plus, RefreshCw } from 'lucide-react'

export default function PagesManagement() {
  useEffect(() => { document.title = 'Town OS - Pages' }, [])
  const [createDialog, setCreateDialog] = useState(false)
  const [editDialog, setEditDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [rebuildConfirm, setRebuildConfirm] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('name')
  const [sortDirection, setSortDirection] = useState('asc')
  const [search, setSearch] = useState('')

  const PAGE_SIZE = 20

  const [pageData, refresh, loading] = usePolling(
    () => getClient().listPages(sortKey, sortDirection, PAGE_SIZE, page * PAGE_SIZE, search || undefined),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey, sortKey, sortDirection, page, search],
  )
  const pages = pageData.entries || []

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function handleCreate(e) {
    e.preventDefault()
    const form = e.target.elements
    const name = form.name.value.trim()
    const repoURL = form.repo_url.value.trim()
    const branch = form.branch.value.trim() || 'main'
    const domain = form.domain.value.trim()

    if (!name) {
      toast.error('Name is required')
      return
    }
    if (!repoURL) {
      toast.error('Repository URL is required')
      return
    }

    try {
      await getClient().createPage(name, repoURL, branch, domain)
      toast.success(`Page "${name}" created`)
      setCreateDialog(false)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleUpdate(e) {
    e.preventDefault()
    const form = e.target.elements
    const fields = {}
    const repoURL = form.repo_url.value.trim()
    const branch = form.branch.value.trim()
    const domain = form.domain.value.trim()

    if (repoURL) fields.repo_url = repoURL
    if (branch) fields.branch = branch
    if (domain) fields.domain = domain

    try {
      await getClient().updatePage(editDialog.name, fields)
      toast.success(`Page "${editDialog.name}" updated`)
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleDelete() {
    try {
      await getClient().removePage(deleteConfirm.name)
      toast.success(`Page "${deleteConfirm.name}" deleted`)
      setDeleteConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setDeleteConfirm(null)
    }
  }

  async function handleRebuild() {
    try {
      await getClient().rebuildPage(rebuildConfirm.name)
      toast.success(`Page "${rebuildConfirm.name}" rebuilt`)
      setRebuildConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setRebuildConfirm(null)
    }
  }

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'domain', label: 'Domain' },
    { key: 'repo_url', label: 'Repository', transform: (v) => (
      <span className="font-mono text-xs truncate block max-w-[250px]" title={v}>{v}</span>
    )},
    { key: 'branch', label: 'Branch', transform: (v) => (
      <Badge variant="outline">{v || 'main'}</Badge>
    )},
    {
      key: 'status',
      label: 'Status',
      transform: (v) => (
        <Badge variant={v === 'active' ? 'default' : v === 'error' ? 'destructive' : 'secondary'}>
          {v}
        </Badge>
      ),
    },
    {
      key: '_actions',
      label: 'Actions',
      sortable: false,
      transform: (_, row) => (
        <div className="flex gap-1 justify-end">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setRebuildConfirm(row)}
            title="Rebuild from git"
          >
            <RefreshCw className="h-3 w-3" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setEditDialog({ open: true, ...row })}
          >
            Edit
          </Button>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setDeleteConfirm(row)}
          >
            Delete
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
          <p className="text-muted-foreground">Manage static HTML content sites</p>
        </div>
        <Button onClick={() => setCreateDialog(true)}>
          <Plus className="h-4 w-4 mr-1" />
          Create Page
        </Button>
      </div>

      {loading && pages.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      <DataTable
        data={pages}
        columns={columns}
        entryKey="name"
        page={page}
        setPage={setPage}
        pageSize={PAGE_SIZE}
        hasMore={pageData.has_more}
        totalPages={pageData.total_pages}
        totalCount={pageData.total_count}
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

      {/* Create dialog */}
      <Dialog open={createDialog} onOpenChange={(v) => !v && setCreateDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Page</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleCreate}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="create-name">Name</Label>
                <Input id="create-name" name="name" placeholder="my-site" required />
              </div>
              <div className="space-y-2">
                <Label htmlFor="create-repo">Repository URL</Label>
                <Input id="create-repo" name="repo_url" placeholder="https://github.com/user/repo.git" required />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="create-branch">Branch</Label>
                  <Input id="create-branch" name="branch" placeholder="main" />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="create-domain">Domain</Label>
                  <Input id="create-domain" name="domain" placeholder="Optional (defaults to name)" />
                </div>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setCreateDialog(false)}>
                Cancel
              </Button>
              <Button type="submit">Create</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit dialog */}
      <Dialog open={editDialog.open} onOpenChange={(v) => !v && setEditDialog({ open: false })}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit Page: {editDialog.name}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleUpdate}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="edit-repo">Repository URL</Label>
                <Input id="edit-repo" name="repo_url" defaultValue={editDialog.repo_url || ''} />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="edit-branch">Branch</Label>
                  <Input id="edit-branch" name="branch" defaultValue={editDialog.branch || ''} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="edit-domain">Domain</Label>
                  <Input id="edit-domain" name="domain" defaultValue={editDialog.domain || ''} />
                </div>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setEditDialog({ open: false })}>
                Cancel
              </Button>
              <Button type="submit">Save Changes</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete confirm */}
      <ConfirmDialog
        open={!!deleteConfirm}
        title="Delete Page"
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
        confirmLabel="Delete"
        variant="destructive"
      >
        Are you sure you want to delete page{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">{deleteConfirm?.name}</code>?
        This will also remove the cloned repository data.
      </ConfirmDialog>

      {/* Rebuild confirm */}
      <ConfirmDialog
        open={!!rebuildConfirm}
        title="Rebuild Page"
        onConfirm={handleRebuild}
        onCancel={() => setRebuildConfirm(null)}
        confirmLabel="Rebuild"
      >
        Pull the latest content from the git repository for{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">{rebuildConfirm?.name}</code>?
      </ConfirmDialog>
    </div>
  )
}
