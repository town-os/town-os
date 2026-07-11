import { useEffect, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import ConfirmDialog from '@/components/ConfirmDialog.jsx'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import { Loader2 } from 'lucide-react'

export default function PagesDialogs({
  createDialog, setCreateDialog, createSourceType, setCreateSourceType,
  provisioning, handleCreate,
  editDialog, setEditDialog, handleUpdate,
  uploadDialog, setUploadDialog, uploading, handleUpload,
  deleteConfirm, setDeleteConfirm, handleDelete,
  rebuildConfirm, setRebuildConfirm, handleRebuild,
  SOURCE_TYPE_LABELS,
}) {
  const { t } = useI18n()
  const [networks, setNetworks] = useState([])

  // Populate the network picker the same way the package install dialog does:
  // enabled networks only, refreshed each time the create dialog opens.
  useEffect(() => {
    if (!createDialog) return
    const client = getClient()
    if (typeof client.listNetworks !== 'function') return
    let cancelled = false
    client
      .listNetworks()
      .then((list) => {
        if (!cancelled) setNetworks((list || []).filter((n) => n.enabled))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [createDialog])

  return (
    <>
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
                <Label htmlFor="create-network">{t('networks.page_label')}</Label>
                <select
                  id="create-network"
                  name="network"
                  defaultValue="home"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                >
                  {(networks.length ? networks : [{ name: 'home' }]).map((n) => (
                    <option key={n.name} value={n.name}>
                      {n.name}
                    </option>
                  ))}
                </select>
                <p className="text-xs text-muted-foreground">{t('networks.page_help')}</p>
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
      <Dialog open={!!uploadDialog} onOpenChange={(v) => { if (!v && !uploading) setUploadDialog(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Upload Archive: {uploadDialog?.name}</DialogTitle>
            <DialogDescription>Upload a new archive to update this page&apos;s content.</DialogDescription>
          </DialogHeader>
          <form onSubmit={handleUpload}>
            <fieldset disabled={uploading}>
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
              <Button variant="outline" type="button" disabled={uploading} onClick={() => setUploadDialog(null)}>
                {t('pages.cancel_btn')}
              </Button>
              <Button type="submit" disabled={uploading}>
                {uploading ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin mr-1" />
                    Uploading...
                  </>
                ) : (
                  'Upload'
                )}
              </Button>
            </DialogFooter>
            </fieldset>
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
    </>
  )
}
