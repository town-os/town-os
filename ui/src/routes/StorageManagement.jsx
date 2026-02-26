import { useState, useEffect, useMemo } from 'react'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Plus, Trash2, Pencil, HardDrive, ChevronRight, ChevronDown, Package, Upload, Download } from 'lucide-react'

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

/**
 * Build a tree structure from package volume names.
 * Names are like "repo/nginx/1.0/data" -> repo "repo", package "nginx", version "1.0", volume "data".
 * The tree groups by "repo/name" as the top-level key.
 * Returns { [repo/name]: { [version]: Filesystem[] } }
 */
function buildVolumeTree(filesystems) {
  const tree = {}
  for (const fs of filesystems) {
    const parts = fs.name.split('/')
    // 4-part: repo/name/version/volName
    if (parts.length >= 4) {
      const pkgKey = `${parts[0]}/${parts[1]}`
      const version = parts[2]
      const volName = parts.slice(3).join('/')
      if (!tree[pkgKey]) tree[pkgKey] = {}
      if (!tree[pkgKey][version]) tree[pkgKey][version] = []
      tree[pkgKey][version].push({ ...fs, volumeName: volName })
    } else {
      // Legacy 3-part: name/version/volName
      const pkgName = parts[0] || fs.name
      const version = parts.length > 1 ? parts[1] : ''
      const volName = parts.length > 2 ? parts.slice(2).join('/') : ''
      if (!tree[pkgName]) tree[pkgName] = {}
      if (!tree[pkgName][version]) tree[pkgName][version] = []
      tree[pkgName][version].push({ ...fs, volumeName: volName })
    }
  }
  return tree
}

function VolumeTreeSection({ title, state, filesystems, badge }) {
  const [expanded, setExpanded] = useState({})
  const tree = useMemo(() => buildVolumeTree(filesystems), [filesystems])
  const packageNames = Object.keys(tree).sort()

  if (packageNames.length === 0) return null

  function togglePkg(pkg) {
    setExpanded((prev) => ({ ...prev, [pkg]: !prev[pkg] }))
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Package className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-lg font-semibold">{title}</h3>
        {badge}
        <span className="text-sm text-muted-foreground ml-auto">
          {filesystems.length} volume{filesystems.length !== 1 ? 's' : ''}
        </span>
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead style={{ width: '50%' }}>Name</TableHead>
              <TableHead style={{ width: '25%' }}>Quota</TableHead>
              <TableHead className="text-right" style={{ width: '25%' }}>
                <div className="flex items-center justify-end pr-2">State</div>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {packageNames.map((pkg) => {
              const versions = Object.keys(tree[pkg]).sort()
              const isExpanded = !!expanded[pkg]
              const totalVols = versions.reduce((sum, v) => sum + tree[pkg][v].length, 0)
              return (
                <>{/* Fragment for package group */}
                  <TableRow
                    key={pkg}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => togglePkg(pkg)}
                  >
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-1">
                        {isExpanded
                          ? <ChevronDown className="h-4 w-4 text-muted-foreground" />
                          : <ChevronRight className="h-4 w-4 text-muted-foreground" />}
                        <span className="font-mono text-sm">{pkg}</span>
                        <span className="text-xs text-muted-foreground ml-2">
                          ({totalVols} volume{totalVols !== 1 ? 's' : ''}, {versions.length} version{versions.length !== 1 ? 's' : ''})
                        </span>
                      </div>
                    </TableCell>
                    <TableCell />
                    <TableCell className="text-right">
                      <Badge variant={state === 'installed' ? 'default' : 'secondary'}>
                        {state}
                      </Badge>
                    </TableCell>
                  </TableRow>
                  {isExpanded && versions.map((version) =>
                    tree[pkg][version].map((vol) => (
                      <TableRow key={`${pkg}/${version}/${vol.volumeName}`}>
                        <TableCell>
                          <span className="font-mono text-sm pl-8 text-muted-foreground">
                            {version}{vol.volumeName ? `/${vol.volumeName}` : ''}
                          </span>
                        </TableCell>
                        <TableCell>{formatQuota(vol.quota)}</TableCell>
                        <TableCell />
                      </TableRow>
                    ))
                  )}
                </>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

export default function StorageManagement() {
  useEffect(() => { document.title = 'Town OS - Storage' }, [])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [uploadDialog, setUploadDialog] = useState({ open: false })
  const [downloadDialog, setDownloadDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('name')
  const [sortDirection, setSortDirection] = useState('asc')
  const [showAll, setShowAll] = useState(false)
  const [search, setSearch] = useState('')

  const PAGE_SIZE = 20

  const [userData, , userLoading] = usePolling(
    () => getClient().listFilesystems('', sortKey, sortDirection, 'user', PAGE_SIZE, page * PAGE_SIZE, search || undefined),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey, sortKey, sortDirection, page, search],
  )
  const userFilesystems = userData.entries || []

  const [installedData, , installedLoading] = usePolling(
    () => getClient().listFilesystems('', 'name', 'asc', 'installed'),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey],
  )
  const installedFilesystems = installedData.entries || []

  const [uninstalledData, , uninstalledLoading] = usePolling(
    () => getClient().listFilesystems('', 'name', 'asc', 'uninstalled'),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey],
  )
  const uninstalledFilesystems = uninstalledData.entries || []

  const loading = userLoading || installedLoading || uninstalledLoading

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

  async function handleUpload(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      const file = form.archive.files[0]
      if (!file) {
        toast.error('Please select an archive file')
        return
      }
      const result = await getClient().uploadArchive(form.subvolume.value, file)
      toast.success(result.message || 'Archive uploaded')
      setUploadDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleDownload(e) {
    e.preventDefault()
    const form = e.target.elements
    const subvolume = form.subvolume.value.trim()
    if (!subvolume) {
      toast.error('Enter a subvolume name')
      return
    }
    const paths = (form.paths?.value || '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const stopService = form.stopService?.value || ''
    try {
      const resp = await getClient().downloadArchive(subvolume, paths.length > 0 ? paths : undefined, stopService)
      if (window.showSaveFilePicker) {
        const handle = await window.showSaveFilePicker({
          suggestedName: 'download.tar.gz',
          types: [{ description: 'Gzip Archive', accept: { 'application/gzip': ['.tar.gz'] } }],
        })
        const writable = await handle.createWritable()
        await resp.body.pipeTo(writable)
      } else {
        const blob = await resp.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = 'download.tar.gz'
        a.click()
        URL.revokeObjectURL(url)
      }
      toast.success('Archive downloaded')
      setDownloadDialog({ open: false })
    } catch (err) {
      if (err.name === 'AbortError') return
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
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
            <input
              type="checkbox"
              checked={showAll}
              onChange={(e) => setShowAll(e.target.checked)}
              className="rounded border-input"
            />
            Show uninstalled volumes
          </label>
          <Button variant="outline" onClick={() => setUploadDialog({ open: true })}>
            <Upload className="h-4 w-4 mr-1" />
            Upload Archive
          </Button>
          <Button variant="outline" onClick={() => setDownloadDialog({ open: true })}>
            <Download className="h-4 w-4 mr-1" />
            Download Archive
          </Button>
          <Button
            onClick={() =>
              setEditDialog({ open: true, create: true, name: '', quotaValue: '', quotaUnit: 'GB' })
            }
          >
            <Plus className="h-4 w-4 mr-1" />
            Create Filesystem
          </Button>
        </div>
      </div>

      {loading && userFilesystems.length === 0 && installedFilesystems.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">Loading...</div>
      )}

      {/* User filesystems section */}
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-lg font-semibold">User Filesystems</h3>
          <span className="text-sm text-muted-foreground ml-auto">
            {userFilesystems.length} filesystem{userFilesystems.length !== 1 ? 's' : ''}
          </span>
        </div>
        <DataTable
          data={userFilesystems}
          columns={columns}
          entryKey="name"
          page={page}
          setPage={setPage}
          pageSize={PAGE_SIZE}
          hasMore={userData.has_more}
          totalPages={userData.total_pages}
          totalCount={userData.total_count}
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

      {/* Installed package volumes section */}
      <VolumeTreeSection
        title="Installed Package Volumes"
        state="installed"
        filesystems={installedFilesystems}
        badge={<Badge variant="default">installed</Badge>}
      />

      {/* Uninstalled package volumes section (only when showAll is checked) */}
      {showAll && (
        <VolumeTreeSection
          title="Uninstalled Package Volumes"
          state="uninstalled"
          filesystems={uninstalledFilesystems}
          badge={<Badge variant="secondary">uninstalled</Badge>}
        />
      )}

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

      <Dialog
        open={uploadDialog.open}
        onOpenChange={(v) => !v && setUploadDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Upload className="h-4 w-4 inline mr-2" />
              Upload Archive
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleUpload}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="subvolume">Target Subvolume</Label>
                <Input
                  id="subvolume"
                  name="subvolume"
                  placeholder="my-data"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="archive">Archive File</Label>
                <Input
                  id="archive"
                  name="archive"
                  type="file"
                  accept=".tar,.tar.gz,.tgz,.tar.bz2,.tbz2,.tar.xz,.txz"
                  required
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setUploadDialog({ open: false })}
              >
                Cancel
              </Button>
              <Button type="submit">Upload</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={downloadDialog.open}
        onOpenChange={(v) => !v && setDownloadDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Download className="h-4 w-4 inline mr-2" />
              Download Archive
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleDownload}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="subvolume">Subvolume</Label>
                <Input
                  id="subvolume"
                  name="subvolume"
                  placeholder="my-data"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="paths">Paths (optional, comma-separated)</Label>
                <Input
                  id="paths"
                  name="paths"
                  placeholder="data, config"
                />
                <p className="text-sm text-muted-foreground">
                  Leave empty to archive the entire subvolume
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="stopService">Stop Service (optional)</Label>
                <Input
                  id="stopService"
                  name="stopService"
                  placeholder="my-app.service"
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setDownloadDialog({ open: false })}
              >
                Cancel
              </Button>
              <Button type="submit">Download</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
