import { useState, useEffect } from 'react'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
import { UNITS, formatQuota, decomposeQuota, deriveServiceName } from '@/lib/storage-utils.jsx'
import { useI18n } from '@/i18n/I18nContext.jsx'
import DataTable from '@/components/DataTable.jsx'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { FORMAT_OPTIONS, DownloadArchiveDialog, UploadArchiveDialog } from '@/components/storage/ArchiveDialogs.jsx'
import VolumeModifyDialog from '@/components/storage/VolumeModifyDialog.jsx'
import PackageVolumeTree from '@/components/storage/PackageVolumeTree.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Plus, Trash2, Pencil, HardDrive } from 'lucide-react'

export default function StorageManagement() {
  const { t } = useI18n()
  useEffect(() => { document.title = t('storage.page_title') }, [t])
  const [editDialog, setEditDialog] = useState({ open: false })
  const [volumeModifyDialog, setVolumeModifyDialog] = useState({ open: false })
  const [downloadDialog, setDownloadDialog] = useState({ open: false })
  const [uploadDialog, setUploadDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [deletePkgVolume, setDeletePkgVolume] = useState(null)
  const [deletePkgGroup, setDeletePkgGroup] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('name')
  const [sortDirection, setSortDirection] = useState('asc')
  const [showAll, setShowAll] = useState(false)
  const [search, setSearch] = useState('')
  const [defaultQuota, setDefaultQuota] = useState({ value: '', unit: 'GB' })

  useEffect(() => {
    getClient().getSetting('default_quota').then((raw) => {
      const bytes = parseInt(raw, 10)
      if (bytes > 0) {
        const [v, u] = decomposeQuota(bytes)
        setDefaultQuota({ value: v, unit: u })
      }
    }).catch(() => {})
  }, [])

  const [userData, , userLoading] = usePolling(
    () => getClient().listFilesystems('', sortKey, sortDirection, 'user', PAGE_SIZE, page * PAGE_SIZE, search || undefined),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey, sortKey, sortDirection, page, search],
  )
  const userFilesystems = userData.entries || []

  const [packageGroups, , pkgLoading] = usePolling(
    () => getClient().listPackageVolumes(showAll),
    [],
    [refreshKey, showAll],
  )

  const loading = userLoading || pkgLoading

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
      toast.success(t('storage.toast_created'))
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
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
      toast.success(t('storage.toast_modified'))
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleDelete() {
    try {
      await getClient().removeFilesystem(deleteConfirm)
      toast.success(t('storage.toast_removed'))
      setDeleteConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setDeleteConfirm(null)
    }
  }

  async function handleDeletePkgVolume() {
    try {
      await getClient().removePackageVolume(deletePkgVolume)
      toast.success(t('storage.toast_pkg_volume_removed'))
      setDeletePkgVolume(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setDeletePkgVolume(null)
    }
  }

  async function handleDeletePkgGroup() {
    if (!deletePkgGroup) return
    try {
      await getClient().removePackageVolumeGroup({
        repo: deletePkgGroup.repo,
        name: deletePkgGroup.effectiveName,
        version: deletePkgGroup.version || '',
        includeUninstalled: showAll,
      })
      toast.success(t('storage.toast_pkg_group_removed'))
      setDeletePkgGroup(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setDeletePkgGroup(null)
    }
  }

  function openDownloadDialog(vol) {
    setDownloadDialog({
      open: true,
      displayName: vol.name,
      internalName: vol.internal_name,
      serviceName: deriveServiceName(vol.internal_name),
    })
  }

  function openUploadDialog(vol) {
    setUploadDialog({
      open: true,
      displayName: vol.name,
      internalName: vol.internal_name,
      serviceName: deriveServiceName(vol.internal_name),
    })
  }

  function openVolumeModifyDialog(vol) {
    const [qv, qu] = decomposeQuota(vol.quota)
    const serviceName = deriveServiceName(vol.internal_name)
    setVolumeModifyDialog({
      open: true,
      displayName: vol.name,
      internalName: vol.internal_name,
      state: vol.state,
      quota: vol.quota,
      quotaValue: qv,
      quotaUnit: qu,
      serviceName,
    })
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
      toast.success(t('storage.toast_archive_downloaded'))
      setDownloadDialog({ open: false })
    } catch (err) {
      if (err.name === 'AbortError') return
      toast.error(err.detail || err.message)
    }
  }

  async function handleUpload(e) {
    e.preventDefault()
    const form = e.target.elements
    const file = form.uploadArchive.files[0]
    if (!file) {
      toast.error(t('storage.toast_upload_no_file'))
      return
    }
    const subvolume = uploadDialog.internalName
    const subpath = form.uploadSubpath?.value || ''
    const stopService = form.uploadStopService?.checked ? uploadDialog.serviceName : ''
    try {
      const result = await getClient().uploadArchive(subvolume, file, subpath, stopService)
      toast.success(result.message || t('storage.toast_archive_uploaded'))
      setUploadDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleVolumeModifyProps(e) {
    e.preventDefault()
    const form = e.target.elements
    try {
      const raw = form.modifyQuota?.value ? parseFloat(form.modifyQuota.value) : 0
      const unit = form.modifyQuotaUnit?.value || 'GB'
      const quota = raw === 0 ? 0 : Math.round(raw * (UNITS[unit] || 1))
      const newName = volumeModifyDialog.state === 'user'
        ? (form.modifyName?.value || volumeModifyDialog.displayName)
        : volumeModifyDialog.internalName
      await getClient().modifyFilesystem(volumeModifyDialog.internalName, {
        name: newName,
        quota,
      })
      toast.success(t('storage.toast_volume_modified'))
      setVolumeModifyDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  const columns = [
    { key: 'name', label: t('storage.col_name') },
    {
      key: 'quota',
      label: t('storage.col_quota'),
      transform: (v) => formatQuota(v),
    },
    {
      key: '_modify',
      label: t('storage.col_modify'),
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
          {t('storage.modify_btn')}
        </Button>
      ),
    },
    {
      key: '_delete',
      label: t('storage.col_delete'),
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
          <h1 className="text-3xl font-bold tracking-tight">{t('storage.title')}</h1>
          <p className="text-muted-foreground">
            {t('storage.description')}
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
            {t('storage.show_uninstalled')}
          </label>
          <Button
            onClick={() =>
              setEditDialog({ open: true, create: true, name: '', quotaValue: defaultQuota.value, quotaUnit: defaultQuota.unit })
            }
          >
            <Plus className="h-4 w-4 mr-1" />
            {t('storage.create_btn')}
          </Button>
        </div>
      </div>

      {loading && userFilesystems.length === 0 && packageGroups.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('storage.loading')}</div>
      )}

      {/* User filesystems section */}
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-lg font-semibold">{t('storage.user_filesystems_title')}</h3>
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
        packageGroups={packageGroups}
        onModifyVolume={openVolumeModifyDialog}
        onDownloadVolume={(vol) => openDownloadDialog(vol)}
        onUploadVolume={(vol) => openUploadDialog(vol)}
        onDeleteVolume={(internalName) => setDeletePkgVolume(internalName)}
        onDeletePackage={(group) => setDeletePkgGroup({
          repo: group.repo,
          effectiveName: group.effective_name || group.package,
          displayName: group.package,
          version: '',
        })}
        onDeleteVersion={(group, version) => setDeletePkgGroup({
          repo: group.repo,
          effectiveName: group.effective_name || group.package,
          displayName: group.package,
          version,
        })}
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
              {editDialog.create ? t('storage.dialog_create_title') : t('storage.dialog_modify_title')}
            </DialogTitle>
            <DialogDescription>
              {editDialog.create ? t('storage.dialog_create_description') : t('storage.dialog_modify_description')}
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={editDialog.create ? handleCreate : handleModify}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">{t('storage.name_label')}</Label>
                <Input
                  id="name"
                  name="name"
                  defaultValue={editDialog.name || ''}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="quota">{t('storage.quota_label')}</Label>
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
                {t('storage.cancel_btn')}
              </Button>
              <Button type="submit">
                {editDialog.create ? t('storage.create_submit') : t('storage.save_changes')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!deleteConfirm}
        title={t('storage.delete_dialog_title')}
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
        confirmLabel={t('storage.delete_confirm_btn')}
        variant="destructive"
      >
        {t('storage.delete_confirm_message', { name: deleteConfirm })}
      </ConfirmDialog>

      <ConfirmDialog
        open={!!deletePkgVolume}
        title={t('storage.delete_pkg_volume_title')}
        onConfirm={handleDeletePkgVolume}
        onCancel={() => setDeletePkgVolume(null)}
        confirmLabel={t('storage.delete_confirm_btn')}
        variant="destructive"
      >
        {t('storage.delete_pkg_volume_message', { name: deletePkgVolume })}
      </ConfirmDialog>

      <ConfirmDialog
        open={!!deletePkgGroup}
        title={t('storage.delete_pkg_group_title')}
        onConfirm={handleDeletePkgGroup}
        onCancel={() => setDeletePkgGroup(null)}
        confirmLabel={t('storage.delete_confirm_btn')}
        variant="destructive"
      >
        {deletePkgGroup?.version
          ? t('storage.delete_pkg_group_message_version', { name: deletePkgGroup.displayName, version: deletePkgGroup.version })
          : t('storage.delete_pkg_group_message_package', { name: deletePkgGroup?.displayName || '' })}
      </ConfirmDialog>

      <DownloadArchiveDialog
        open={downloadDialog.open}
        dialog={downloadDialog}
        onClose={() => setDownloadDialog({ open: false })}
        onSubmit={handleDownload}
      />

      <UploadArchiveDialog
        open={uploadDialog.open}
        dialog={uploadDialog}
        onClose={() => setUploadDialog({ open: false })}
        onSubmit={handleUpload}
      />

      <VolumeModifyDialog
        open={volumeModifyDialog.open}
        dialog={volumeModifyDialog}
        onClose={() => setVolumeModifyDialog({ open: false })}
        onSubmit={handleVolumeModifyProps}
      />
    </div>
  )
}
