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
 * Derive the systemd service unit name from a volume display name.
 * e.g. "repo/name/version/volName" -> "town-os-package--repo-name-version.service"
 */
function deriveServiceName(volumeName) {
  const parts = volumeName.split('/')
  if (parts.length < 3) return ''
  return `town-os-package--${parts[0]}-${parts[1]}-${parts[2]}.service`
}

/**
 * Build a unified tree structure from package volume names with state info.
 * Merges installed and uninstalled volumes into a single tree.
 * Returns { [repo/name]: { [version]: { state, volumes: Filesystem[] } } }
 */
function buildUnifiedVolumeTree(installedFilesystems, uninstalledFilesystems) {
  const tree = {}

  function addToTree(filesystems, state) {
    for (const fs of filesystems) {
      const parts = fs.name.split('/')
      if (parts.length >= 4) {
        const pkgKey = `${parts[0]}/${parts[1]}`
        const version = parts[2]
        const volName = parts.slice(3).join('/')
        if (!tree[pkgKey]) tree[pkgKey] = {}
        if (!tree[pkgKey][version]) tree[pkgKey][version] = { state, volumes: [] }
        tree[pkgKey][version].volumes.push({ ...fs, volumeName: volName, state })
      } else {
        const pkgName = parts[0] || fs.name
        const version = parts.length > 1 ? parts[1] : ''
        const volName = parts.length > 2 ? parts.slice(2).join('/') : ''
        if (!tree[pkgName]) tree[pkgName] = {}
        if (!tree[pkgName][version]) tree[pkgName][version] = { state, volumes: [] }
        tree[pkgName][version].volumes.push({ ...fs, volumeName: volName, state })
      }
    }
  }

  addToTree(installedFilesystems, 'installed')
  addToTree(uninstalledFilesystems, 'uninstalled')
  return tree
}

/**
 * Compute total quota across all volumes in a version group or package group.
 */
function sumQuota(volumes) {
  return volumes.reduce((sum, v) => sum + (v.quota || 0), 0)
}

function PackageVolumeTree({ installedFilesystems, uninstalledFilesystems, showUninstalled, onModifyVolume, onDownloadVolume, onUploadVolume }) {
  const [expanded, setExpanded] = useState({})
  const tree = useMemo(
    () => buildUnifiedVolumeTree(installedFilesystems, showUninstalled ? uninstalledFilesystems : []),
    [installedFilesystems, uninstalledFilesystems, showUninstalled],
  )
  const packageNames = Object.keys(tree).sort()

  const totalVolumes = installedFilesystems.length + (showUninstalled ? uninstalledFilesystems.length : 0)

  if (packageNames.length === 0) return null

  function togglePkg(pkg) {
    setExpanded((prev) => ({ ...prev, [pkg]: !prev[pkg] }))
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Package className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-lg font-semibold">Package Volumes</h3>
        <span className="text-sm text-muted-foreground ml-auto">
          {totalVolumes} volume{totalVolumes !== 1 ? 's' : ''}
        </span>
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead style={{ width: '40%' }}>Name</TableHead>
              <TableHead style={{ width: '20%' }}>Quota</TableHead>
              <TableHead style={{ width: '15%' }}>State</TableHead>
              <TableHead className="text-right" style={{ width: '25%' }}>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {packageNames.map((pkg) => {
              const versions = Object.keys(tree[pkg]).sort()
              const isExpanded = !!expanded[pkg]
              const allVolumes = versions.flatMap((v) => tree[pkg][v].volumes)
              const totalVols = allVolumes.length
              const totalQ = sumQuota(allVolumes)
              const states = [...new Set(versions.map((v) => tree[pkg][v].state))]
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
                    <TableCell className="text-sm text-muted-foreground">
                      {totalQ > 0 ? formatQuotaText(totalQ) : ''}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {states.map((s) => (
                          <Badge key={s} variant={s === 'installed' ? 'default' : 'secondary'}>{s}</Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell />
                  </TableRow>
                  {isExpanded && versions.map((version) => {
                    const versionState = tree[pkg][version].state
                    const versionVolumes = tree[pkg][version].volumes
                    const versionQ = sumQuota(versionVolumes)
                    return (
                      <>{/* Version header */}
                        {versions.length > 1 && (
                          <TableRow key={`${pkg}/${version}`} className="bg-muted/30">
                            <TableCell>
                              <span className="font-mono text-sm pl-6 font-medium text-muted-foreground">
                                v{version}
                              </span>
                            </TableCell>
                            <TableCell className="text-sm text-muted-foreground">
                              {versionQ > 0 ? formatQuotaText(versionQ) : ''}
                            </TableCell>
                            <TableCell>
                              <Badge variant={versionState === 'installed' ? 'default' : 'secondary'}>
                                {versionState}
                              </Badge>
                            </TableCell>
                            <TableCell />
                          </TableRow>
                        )}
                        {versionVolumes.map((vol) => (
                          <TableRow key={`${pkg}/${version}/${vol.volumeName}`}>
                            <TableCell>
                              <span className="font-mono text-sm pl-8 text-muted-foreground">
                                {versions.length > 1 ? '' : `${version}/`}{vol.volumeName}
                              </span>
                            </TableCell>
                            <TableCell>{formatQuota(vol.quota)}</TableCell>
                            <TableCell>
                              {versions.length <= 1 && (
                                <Badge variant={vol.state === 'installed' ? 'default' : 'secondary'}>
                                  {vol.state}
                                </Badge>
                              )}
                            </TableCell>
                            <TableCell className="text-right">
                              <div className="flex items-center justify-end gap-1">
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-7 w-7"
                                  title="Download archive"
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    onDownloadVolume(vol, pkg, version)
                                  }}
                                >
                                  <Download className="h-3 w-3" />
                                </Button>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-7 w-7"
                                  title="Upload archive"
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    onUploadVolume(vol, pkg, version)
                                  }}
                                >
                                  <Upload className="h-3 w-3" />
                                </Button>
                                <Button
                                  variant="outline"
                                  size="sm"
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    onModifyVolume(vol, pkg, version)
                                  }}
                                >
                                  <Pencil className="h-3 w-3 mr-1" />
                                  Modify
                                </Button>
                              </div>
                            </TableCell>
                          </TableRow>
                        ))}
                      </>
                    )
                  })}
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
  const [volumeModifyDialog, setVolumeModifyDialog] = useState({ open: false })
  const [downloadDialog, setDownloadDialog] = useState({ open: false })
  const [uploadDialog, setUploadDialog] = useState({ open: false })
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

  function volumeInternalName(vol) {
    const prefix = vol.state === 'installed' ? 'installed/' : 'uninstalled/'
    return prefix + vol.name
  }

  function openDownloadDialog(vol) {
    setDownloadDialog({
      open: true,
      displayName: vol.name,
      internalName: volumeInternalName(vol),
      serviceName: deriveServiceName(vol.name),
    })
  }

  function openUploadDialog(vol) {
    setUploadDialog({
      open: true,
      displayName: vol.name,
      internalName: volumeInternalName(vol),
      serviceName: deriveServiceName(vol.name),
    })
  }

  function openVolumeModifyDialog(vol, pkg, version) {
    const [qv, qu] = decomposeQuota(vol.quota)
    const serviceName = deriveServiceName(vol.name)
    setVolumeModifyDialog({
      open: true,
      displayName: vol.name,
      internalName: volumeInternalName(vol),
      volumeName: vol.volumeName,
      pkg,
      version,
      state: vol.state,
      quota: vol.quota,
      quotaValue: qv,
      quotaUnit: qu,
      serviceName,
    })
  }

  const FORMAT_OPTIONS = {
    'tar.gz': { ext: '.tar.gz', desc: 'Gzip Archive', mime: 'application/gzip' },
    'tar.bz2': { ext: '.tar.bz2', desc: 'Bzip2 Archive', mime: 'application/x-bzip2' },
    'tar.xz': { ext: '.tar.xz', desc: 'XZ Archive', mime: 'application/x-xz' },
  }

  async function handleDownload(e) {
    e.preventDefault()
    const form = e.target.elements
    const subvolume = downloadDialog.internalName
    const paths = (form.downloadPaths?.value || '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const stopService = form.downloadStopService?.checked ? downloadDialog.serviceName : ''
    const format = form.downloadFormat?.value || 'tar.gz'
    const info = FORMAT_OPTIONS[format] || FORMAT_OPTIONS['tar.gz']
    const customFilename = (form.downloadFilename?.value || '').trim()
    try {
      const resp = await getClient().downloadArchive(subvolume, paths.length > 0 ? paths : undefined, stopService, format, customFilename || undefined)
      const filename = (customFilename || 'download') + info.ext
      if (window.showSaveFilePicker) {
        const handle = await window.showSaveFilePicker({
          suggestedName: filename,
          types: [{ description: info.desc, accept: { [info.mime]: [info.ext] } }],
        })
        const writable = await handle.createWritable()
        await resp.body.pipeTo(writable)
      } else {
        const blob = await resp.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = filename
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

  async function handleUpload(e) {
    e.preventDefault()
    const form = e.target.elements
    const file = form.uploadArchive.files[0]
    if (!file) {
      toast.error('Please select an archive file')
      return
    }
    const subvolume = uploadDialog.internalName
    const subpath = form.uploadSubpath?.value || ''
    const stopService = form.uploadStopService?.checked ? uploadDialog.serviceName : ''
    try {
      const result = await getClient().uploadArchive(subvolume, file, subpath, stopService)
      toast.success(result.message || 'Archive uploaded')
      setUploadDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
    }
  }

  async function handleVolumeModifyProps(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      const raw = form.modifyQuota?.value ? parseFloat(form.modifyQuota.value) : 0
      const unit = form.modifyQuotaUnit?.value || 'GB'
      const quota = raw === 0 ? 0 : Math.round(raw * (UNITS[unit] || 1))
      const newName = form.modifyName?.value || volumeModifyDialog.displayName
      await getClient().modifyFilesystem(volumeModifyDialog.internalName, {
        name: newName,
        quota,
      })
      toast.success('Volume modified')
      setVolumeModifyDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.message)
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

      {/* Unified package volumes section */}
      <PackageVolumeTree
        installedFilesystems={installedFilesystems}
        uninstalledFilesystems={uninstalledFilesystems}
        showUninstalled={showAll}
        onModifyVolume={openVolumeModifyDialog}
        onDownloadVolume={(vol) => openDownloadDialog(vol)}
        onUploadVolume={(vol) => openUploadDialog(vol)}
      />

      {/* Create/Modify user filesystem dialog */}
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

      {/* Download Archive confirmation dialog */}
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
          <div className="text-sm text-muted-foreground pb-2">
            <span className="font-medium text-foreground">Volume:</span>{' '}
            <code className="text-xs bg-muted px-1 rounded">{downloadDialog.displayName}</code>
          </div>
          <form onSubmit={handleDownload} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="downloadFilename">Filename (optional)</Label>
              <Input
                id="downloadFilename"
                name="downloadFilename"
                placeholder={downloadDialog.displayName || 'download'}
              />
              <p className="text-xs text-muted-foreground">
                Base name for the downloaded file. The archive extension is added automatically.
              </p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="downloadFormat">Compression Format</Label>
              <select
                id="downloadFormat"
                name="downloadFormat"
                defaultValue="tar.gz"
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
              >
                <option value="tar.gz">gzip (.tar.gz)</option>
                <option value="tar.bz2">bzip2 (.tar.bz2)</option>
                <option value="tar.xz">xz (.tar.xz)</option>
              </select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="downloadPaths">Paths (optional, comma-separated)</Label>
              <Input id="downloadPaths" name="downloadPaths" placeholder="data, config" />
              <p className="text-xs text-muted-foreground">Leave empty to archive the entire volume</p>
            </div>
            {downloadDialog.serviceName && (
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" name="downloadStopService" className="rounded border-input" />
                Stop service during download
              </label>
            )}
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setDownloadDialog({ open: false })}>
                Cancel
              </Button>
              <Button type="submit">
                <Download className="h-3 w-3 mr-1" />
                Download
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Upload Archive dialog */}
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
          <div className="text-sm text-muted-foreground pb-2">
            <span className="font-medium text-foreground">Volume:</span>{' '}
            <code className="text-xs bg-muted px-1 rounded">{uploadDialog.displayName}</code>
          </div>
          <form onSubmit={handleUpload} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="uploadArchive">Archive File</Label>
              <Input
                id="uploadArchive"
                name="uploadArchive"
                type="file"
                accept=".tar,.tar.gz,.tgz,.tar.bz2,.tbz2,.tar.xz,.txz"
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="uploadSubpath">Subpath (optional)</Label>
              <Input id="uploadSubpath" name="uploadSubpath" placeholder="relative/path" />
              <p className="text-xs text-muted-foreground">Unpack into a subdirectory within the volume</p>
            </div>
            {uploadDialog.serviceName && (
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" name="uploadStopService" className="rounded border-input" />
                Stop service during upload
              </label>
            )}
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setUploadDialog({ open: false })}>
                Cancel
              </Button>
              <Button type="submit">
                <Upload className="h-3 w-3 mr-1" />
                Upload
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Volume Modify dialog (name & quota for package volumes) */}
      <Dialog
        open={volumeModifyDialog.open}
        onOpenChange={(v) => !v && setVolumeModifyDialog({ open: false })}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Pencil className="h-4 w-4 inline mr-2" />
              Modify Volume
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-1 text-sm text-muted-foreground pb-2">
            <div><span className="font-medium text-foreground">Volume:</span> {volumeModifyDialog.displayName}</div>
            <div><span className="font-medium text-foreground">State:</span>{' '}
              <Badge variant={volumeModifyDialog.state === 'installed' ? 'default' : 'secondary'}>
                {volumeModifyDialog.state}
              </Badge>
            </div>
            {volumeModifyDialog.serviceName && (
              <div><span className="font-medium text-foreground">Service:</span> <code className="text-xs bg-muted px-1 rounded">{volumeModifyDialog.serviceName}</code></div>
            )}
          </div>
          <form onSubmit={handleVolumeModifyProps} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="modifyName">Name</Label>
              <Input
                id="modifyName"
                name="modifyName"
                defaultValue={volumeModifyDialog.displayName || ''}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="modifyQuota">Quota (0 = unlimited)</Label>
              <div className="flex gap-2">
                <Input
                  id="modifyQuota"
                  name="modifyQuota"
                  type="number"
                  min="0"
                  step="any"
                  defaultValue={volumeModifyDialog.quotaValue || ''}
                  placeholder="0"
                  className="flex-1"
                />
                <select
                  name="modifyQuotaUnit"
                  defaultValue={volumeModifyDialog.quotaUnit || 'GB'}
                  className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                >
                  <option value="B">B</option>
                  <option value="MB">MB</option>
                  <option value="GB">GB</option>
                  <option value="TB">TB</option>
                </select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setVolumeModifyDialog({ open: false })}>
                Cancel
              </Button>
              <Button type="submit">Save Changes</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
