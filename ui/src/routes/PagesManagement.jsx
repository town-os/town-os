import { useState, useEffect, useRef } from 'react'
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
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Loader2, Plus, RefreshCw, Upload } from 'lucide-react'

const SOURCE_TYPE_LABELS = {
  archive: 'Archive Upload',
  container_image: 'Container Image',
  git: 'Git Repository',
}

export default function PagesManagement() {
  const { t } = useI18n()
  useEffect(() => { document.title = t('pages.page_title') }, [t])
  const [createDialog, setCreateDialog] = useState(false)
  const [editDialog, setEditDialog] = useState({ open: false })
  const [deleteConfirm, setDeleteConfirm] = useState(null)
  const [rebuildConfirm, setRebuildConfirm] = useState(null)
  const [uploadDialog, setUploadDialog] = useState(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [page, setPage] = useState(0)
  const [sortKey, setSortKey] = useState('name')
  const [sortDirection, setSortDirection] = useState('asc')
  const [search, setSearch] = useState('')
  const [createSourceType, setCreateSourceType] = useState('archive')
  const [provisioning, setProvisioning] = useState(false)
  const cancelPollingRef = useRef(false)

  useEffect(() => {
    return () => { cancelPollingRef.current = true }
  }, [])

  const [pageData, refresh, loading] = usePolling(
    () => getClient().listPages(sortKey, sortDirection, PAGE_SIZE, page * PAGE_SIZE, search || undefined),
    { entries: [], has_more: false, total_pages: 1, total_count: 0 },
    [refreshKey, sortKey, sortDirection, page, search],
  )
  const pages = pageData.entries || []

  function doRefresh() {
    setRefreshKey((k) => k + 1)
  }

  async function pollPageStatus(name) {
    const maxAttempts = 30
    for (let i = 0; i < maxAttempts; i++) {
      if (cancelPollingRef.current) return 'pending'
      await new Promise((r) => setTimeout(r, 2000))
      if (cancelPollingRef.current) return 'pending'
      const result = await getClient().listPages()
      const found = (result.entries || []).find((p) => p.name === name)
      if (found && found.status !== 'pending') return found.status
    }
    return 'pending'
  }

  async function handleCreate(e) {
    e.preventDefault()
    const form = e.target.elements
    const name = form.name.value.trim()
    const domain = form.domain.value.trim()

    if (!name) {
      toast.error(t('pages.error_name_required'))
      return
    }

    try {
      if (createSourceType === 'git') {
        const repoURL = form.repo_url.value.trim()
        const branch = form.branch.value.trim() || 'main'
        if (!repoURL) {
          toast.error(t('pages.error_repo_required'))
          return
        }
        setProvisioning(true)
        cancelPollingRef.current = false
        await getClient().createPage(name, repoURL, branch, domain, 'git', '', '')
        const finalStatus = await pollPageStatus(name)
        if (finalStatus === 'active') {
          toast.success(t('pages.toast_provisioned'))
        } else {
          toast.error(t('pages.toast_provision_failed'))
        }
      } else if (createSourceType === 'container_image') {
        const image = form.image.value.trim()
        const imageDirectory = form.image_directory.value.trim()
        if (!image) {
          toast.error('Container image is required')
          return
        }
        if (!imageDirectory) {
          toast.error('Image directory is required')
          return
        }
        setProvisioning(true)
        cancelPollingRef.current = false
        await getClient().createPage(name, '', '', domain, 'container_image', image, imageDirectory)
        const finalStatus = await pollPageStatus(name)
        if (finalStatus === 'active') {
          toast.success(t('pages.toast_provisioned'))
        } else {
          toast.error(t('pages.toast_provision_failed'))
        }
      } else {
        // archive
        const archiveFile = form.archive?.files?.[0]
        if (archiveFile) {
          setProvisioning(true)
          const created = await getClient().createPage(name, '', '', domain, 'archive', '', '')
          await getClient().uploadPageArchive(created.name, archiveFile)
          toast.success(t('pages.toast_provisioned'))
        } else {
          await getClient().createPage(name, '', '', domain, 'archive', '', '')
          toast.success(t('pages.toast_created'))
        }
      }
      setProvisioning(false)
      setCreateDialog(false)
      setCreateSourceType('archive')
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setProvisioning(false)
    }
  }

  async function handleUpdate(e) {
    e.preventDefault()
    const form = e.target.elements
    const fields = {}
    const repoURL = form.repo_url?.value?.trim()
    const branch = form.branch?.value?.trim()
    const domain = form.domain.value.trim()
    const image = form.image?.value?.trim()
    const imageDirectory = form.image_directory?.value?.trim()

    if (repoURL) fields.repo_url = repoURL
    if (branch) fields.branch = branch
    if (domain) fields.domain = domain
    if (image !== undefined && image !== '') fields.image = image
    if (imageDirectory !== undefined && imageDirectory !== '') fields.image_directory = imageDirectory

    try {
      await getClient().updatePage(editDialog.name, fields)
      toast.success(t('pages.toast_updated'))
      setEditDialog({ open: false })
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  async function handleDelete() {
    try {
      await getClient().removePage(deleteConfirm.name)
      toast.success(t('pages.toast_deleted'))
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
      toast.success(t('pages.toast_rebuilt'))
      setRebuildConfirm(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setRebuildConfirm(null)
    }
  }

  async function handleUpload(e) {
    e.preventDefault()
    const form = e.target.elements
    const archiveFile = form.archive?.files?.[0]
    if (!archiveFile) {
      toast.error('Please select an archive file')
      return
    }

    try {
      await getClient().uploadPageArchive(uploadDialog.name, archiveFile)
      toast.success(`Archive uploaded for "${uploadDialog.name}"`)
      setUploadDialog(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
    }
  }

  const columns = [
    { key: 'name', label: t('pages.col_name') },
    { key: 'domain', label: t('pages.col_domain') },
    {
      key: 'source_type',
      label: 'Source',
      transform: (v) => (
        <Badge variant="outline">{SOURCE_TYPE_LABELS[v] || v}</Badge>
      ),
    },
    { key: 'repo_url', label: t('pages.col_repository'), transform: (v, row) => {
      if (row.source_type === 'git') {
        return <span className="font-mono text-xs truncate block max-w-[200px]" title={v}>{v}</span>
      }
      if (row.source_type === 'container_image') {
        return <span className="font-mono text-xs truncate block max-w-[200px]" title={row.image}>{row.image}</span>
      }
      return <span className="text-muted-foreground text-xs">-</span>
    }},
    { key: 'branch', label: t('pages.col_branch'), transform: (v, row) => {
      if (row.source_type === 'git') {
        return <Badge variant="outline">{v || 'main'}</Badge>
      }
      return <span className="text-muted-foreground text-xs">-</span>
    }},
    {
      key: 'status',
      label: t('pages.col_status'),
      transform: (v) => (
        <Badge variant={v === 'active' ? 'default' : v === 'error' ? 'destructive' : 'secondary'}>
          {v === 'pending' && <Loader2 className="h-3 w-3 animate-spin mr-1" />}
          {v === 'pending' ? t('pages.status_provisioning') : v}
        </Badge>
      ),
    },
    {
      key: '_actions',
      label: t('pages.col_actions'),
      sortable: false,
      transform: (_, row) => (
        <div className="flex gap-1 justify-end">
          {row.source_type === 'archive' ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setUploadDialog(row)}
              title="Upload archive"
            >
              <Upload className="h-3 w-3" />
            </Button>
          ) : (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRebuildConfirm(row)}
              title={row.source_type === 'container_image' ? 'Re-extract from image' : t('pages.rebuild_tooltip')}
            >
              <RefreshCw className="h-3 w-3" />
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => setEditDialog({ open: true, ...row })}
          >
            {t('pages.edit_btn')}
          </Button>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setDeleteConfirm(row)}
          >
            {t('pages.delete_btn')}
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('pages.title')}</h1>
          <p className="text-muted-foreground">{t('pages.description')}</p>
        </div>
        <Button onClick={() => setCreateDialog(true)}>
          <Plus className="h-4 w-4 mr-1" />
          {t('pages.create_btn')}
        </Button>
      </div>

      {loading && pages.length === 0 && (
        <div className="text-center py-8 text-muted-foreground animate-pulse">{t('pages.loading')}</div>
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
      <Dialog open={createDialog} onOpenChange={(v) => { if (!v && !provisioning) { setCreateDialog(false); setCreateSourceType('archive') } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('pages.create_dialog_title')}</DialogTitle>
            <DialogDescription>{t('pages.create_dialog_description')}</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleCreate}>
            <fieldset disabled={provisioning}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="create-source-type">Source Type</Label>
                <Select value={createSourceType} onValueChange={setCreateSourceType}>
                  <SelectTrigger id="create-source-type">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="archive">Archive Upload</SelectItem>
                    <SelectItem value="container_image">Container Image</SelectItem>
                    <SelectItem value="git">Git Repository</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="create-name">{t('pages.name_label')}</Label>
                <Input id="create-name" name="name" placeholder={t('pages.name_placeholder')} required />
              </div>
              {createSourceType === 'git' && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="create-repo">{t('pages.repo_url_label')}</Label>
                    <Input id="create-repo" name="repo_url" placeholder={t('pages.repo_url_placeholder')} required />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="create-branch">{t('pages.branch_label')}</Label>
                    <Input id="create-branch" name="branch" placeholder={t('pages.branch_placeholder')} />
                  </div>
                </>
              )}
              {createSourceType === 'container_image' && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="create-image">Container Image</Label>
                    <Input id="create-image" name="image" placeholder="nginx:latest" required />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="create-image-dir">Image Directory</Label>
                    <Input id="create-image-dir" name="image_directory" placeholder="/usr/share/nginx/html" required />
                  </div>
                </>
              )}
              {createSourceType === 'archive' && (
                <div className="space-y-2">
                  <Label htmlFor="create-archive">Archive File</Label>
                  <Input
                    id="create-archive"
                    name="archive"
                    type="file"
                    accept=".tar,.tar.gz,.tgz,.tar.bz2,.tbz2,.tar.xz,.txz"
                  />
                  <p className="text-xs text-muted-foreground">
                    Supported formats: .tar, .tar.gz, .tar.bz2, .tar.xz. You can also upload later.
                  </p>
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="create-domain">{t('pages.domain_label')}</Label>
                <Input id="create-domain" name="domain" placeholder={t('pages.domain_placeholder')} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" disabled={provisioning} onClick={() => { setCreateDialog(false); setCreateSourceType('archive') }}>
                {t('pages.cancel_btn')}
              </Button>
              <Button type="submit" disabled={provisioning}>
                {provisioning ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin mr-1" />
                    {t('pages.create_submit_provisioning')}
                  </>
                ) : (
                  t('pages.create_submit')
                )}
              </Button>
            </DialogFooter>
            </fieldset>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit dialog */}
      <Dialog open={editDialog.open} onOpenChange={(v) => !v && setEditDialog({ open: false })}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('pages.edit_dialog_title')}: {editDialog.name}</DialogTitle>
            <DialogDescription>{t('pages.edit_dialog_description')} Source type: {SOURCE_TYPE_LABELS[editDialog.source_type] || editDialog.source_type}.</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleUpdate}>
            <div className="space-y-4 py-4">
              {(editDialog.source_type === 'git' || !editDialog.source_type) && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="edit-repo">{t('pages.repo_url_label')}</Label>
                    <Input id="edit-repo" name="repo_url" defaultValue={editDialog.repo_url || ''} />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="edit-branch">{t('pages.branch_label')}</Label>
                    <Input id="edit-branch" name="branch" defaultValue={editDialog.branch || ''} />
                  </div>
                </>
              )}
              {editDialog.source_type === 'container_image' && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="edit-image">Container Image</Label>
                    <Input id="edit-image" name="image" defaultValue={editDialog.image || ''} />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="edit-image-dir">Image Directory</Label>
                    <Input id="edit-image-dir" name="image_directory" defaultValue={editDialog.image_directory || ''} />
                  </div>
                </>
              )}
              <div className="space-y-2">
                <Label htmlFor="edit-domain">{t('pages.domain_label')}</Label>
                <Input id="edit-domain" name="domain" defaultValue={editDialog.domain || ''} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setEditDialog({ open: false })}>
                {t('pages.cancel_btn')}
              </Button>
              <Button type="submit">{t('pages.save_changes')}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Upload archive dialog */}
      <Dialog open={!!uploadDialog} onOpenChange={(v) => !v && setUploadDialog(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Upload Archive: {uploadDialog?.name}</DialogTitle>
            <DialogDescription>Upload a new archive to update this page&apos;s content.</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleUpload}>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="upload-archive">Archive File</Label>
                <Input
                  id="upload-archive"
                  name="archive"
                  type="file"
                  accept=".tar,.tar.gz,.tgz,.tar.bz2,.tbz2,.tar.xz,.txz"
                  required
                />
                <p className="text-xs text-muted-foreground">
                  Supported formats: .tar, .tar.gz, .tar.bz2, .tar.xz
                </p>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => setUploadDialog(null)}>
                {t('pages.cancel_btn')}
              </Button>
              <Button type="submit">Upload</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete confirm */}
      <ConfirmDialog
        open={!!deleteConfirm}
        title={t('pages.delete_dialog_title')}
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
        confirmLabel={t('pages.delete_confirm_btn')}
        variant="destructive"
      >
        Are you sure you want to delete page{' '}
        <code className="font-mono text-sm bg-muted px-1 rounded">{deleteConfirm?.name}</code>?
        This will also remove all associated content data.
      </ConfirmDialog>

      {/* Rebuild confirm */}
      <ConfirmDialog
        open={!!rebuildConfirm}
        title={t('pages.rebuild_dialog_title')}
        onConfirm={handleRebuild}
        onCancel={() => setRebuildConfirm(null)}
        confirmLabel={t('pages.rebuild_confirm_btn')}
      >
        {rebuildConfirm?.source_type === 'container_image'
          ? <>Re-extract content from the container image for{' '}<code className="font-mono text-sm bg-muted px-1 rounded">{rebuildConfirm?.name}</code>?</>
          : <>Pull the latest content from the git repository for{' '}<code className="font-mono text-sm bg-muted px-1 rounded">{rebuildConfirm?.name}</code>?</>
        }
      </ConfirmDialog>
    </div>
  )
}
