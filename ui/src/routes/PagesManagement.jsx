import { useState, useEffect, useRef } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import { usePolling } from '@/lib/hooks.js'
import { PAGE_SIZE } from '@/lib/utils.js'
import DataTable from '@/components/DataTable.jsx'
import PagesDialogs from '@/components/pages/PagesDialogs.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
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
  const [uploading, setUploading] = useState(false)
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
      setUploading(true)
      await getClient().uploadPageArchive(uploadDialog.name, archiveFile)
      toast.success(`Archive uploaded for "${uploadDialog.name}"`)
      setUploading(false)
      setUploadDialog(null)
      doRefresh()
    } catch (err) {
      toast.error(err.detail || err.message)
      setUploading(false)
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

      <PagesDialogs
        createDialog={createDialog} setCreateDialog={setCreateDialog}
        createSourceType={createSourceType} setCreateSourceType={setCreateSourceType}
        provisioning={provisioning} handleCreate={handleCreate}
        editDialog={editDialog} setEditDialog={setEditDialog} handleUpdate={handleUpdate}
        uploadDialog={uploadDialog} setUploadDialog={setUploadDialog}
        uploading={uploading} handleUpload={handleUpload}
        deleteConfirm={deleteConfirm} setDeleteConfirm={setDeleteConfirm} handleDelete={handleDelete}
        rebuildConfirm={rebuildConfirm} setRebuildConfirm={setRebuildConfirm} handleRebuild={handleRebuild}
        SOURCE_TYPE_LABELS={SOURCE_TYPE_LABELS}
      />
    </div>
  )
}
