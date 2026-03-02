import { useI18n } from '@/i18n/I18nContext.jsx'
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
import { Upload, Download } from 'lucide-react'

const FORMAT_OPTIONS = {
  'tar.gz': { ext: '.tar.gz', desc: 'Gzip Archive', mime: 'application/gzip' },
  'tar.bz2': { ext: '.tar.bz2', desc: 'Bzip2 Archive', mime: 'application/x-bzip2' },
  'tar.xz': { ext: '.tar.xz', desc: 'XZ Archive', mime: 'application/x-xz' },
}

function DownloadArchiveDialog({ open, dialog, onClose, onSubmit }) {
  const { t } = useI18n()
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            <Download className="h-4 w-4 inline mr-2" />
            {t('archive.download_title')}
          </DialogTitle>
          <DialogDescription>{t('archive.download_description')}</DialogDescription>
        </DialogHeader>
        <div className="text-sm text-muted-foreground pb-2">
          <span className="font-medium text-foreground">{t('archive.volume_label')}</span>{' '}
          <code className="text-xs bg-muted px-1 rounded">{dialog.displayName}</code>
        </div>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="downloadFilename">{t('archive.filename_label')}</Label>
            <Input
              id="downloadFilename"
              name="downloadFilename"
              placeholder={dialog.displayName || 'download'}
            />
            <p className="text-xs text-muted-foreground">
              {t('archive.filename_hint')}
            </p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="downloadFormat">{t('archive.format_label')}</Label>
            <select
              id="downloadFormat"
              name="downloadFormat"
              defaultValue="tar.gz"
              className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            >
              <option value="tar.gz">{t('archive.format_gzip')}</option>
              <option value="tar.bz2">{t('archive.format_bzip2')}</option>
              <option value="tar.xz">{t('archive.format_xz')}</option>
            </select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="downloadPaths">{t('archive.paths_label')}</Label>
            <Input id="downloadPaths" name="downloadPaths" placeholder={t('archive.paths_placeholder')} />
            <p className="text-xs text-muted-foreground">{t('archive.paths_hint')}</p>
          </div>
          {dialog.serviceName && (
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" name="downloadStopService" className="rounded border-input" />
              {t('archive.stop_service_download')}
            </label>
          )}
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onClose}>
              {t('archive.cancel_btn')}
            </Button>
            <Button type="submit">
              <Download className="h-3 w-3 mr-1" />
              {t('archive.download_btn')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function UploadArchiveDialog({ open, dialog, onClose, onSubmit }) {
  const { t } = useI18n()
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            <Upload className="h-4 w-4 inline mr-2" />
            {t('archive.upload_title')}
          </DialogTitle>
          <DialogDescription>{t('archive.upload_description')}</DialogDescription>
        </DialogHeader>
        <div className="text-sm text-muted-foreground pb-2">
          <span className="font-medium text-foreground">{t('archive.volume_label')}</span>{' '}
          <code className="text-xs bg-muted px-1 rounded">{dialog.displayName}</code>
        </div>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="uploadArchive">{t('archive.archive_file_label')}</Label>
            <Input
              id="uploadArchive"
              name="uploadArchive"
              type="file"
              accept=".tar,.tar.gz,.tgz,.tar.bz2,.tbz2,.tar.xz,.txz"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="uploadSubpath">{t('archive.subpath_label')}</Label>
            <Input id="uploadSubpath" name="uploadSubpath" placeholder={t('archive.subpath_placeholder')} />
            <p className="text-xs text-muted-foreground">{t('archive.subpath_hint')}</p>
          </div>
          {dialog.serviceName && (
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" name="uploadStopService" className="rounded border-input" />
              {t('archive.stop_service_upload')}
            </label>
          )}
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onClose}>
              {t('archive.cancel_btn')}
            </Button>
            <Button type="submit">
              <Upload className="h-3 w-3 mr-1" />
              {t('archive.upload_btn')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export { FORMAT_OPTIONS, DownloadArchiveDialog, UploadArchiveDialog }
