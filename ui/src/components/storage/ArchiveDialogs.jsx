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
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            <Download className="h-4 w-4 inline mr-2" />
            Download Archive
          </DialogTitle>
          <DialogDescription>Download volume contents as a compressed archive.</DialogDescription>
        </DialogHeader>
        <div className="text-sm text-muted-foreground pb-2">
          <span className="font-medium text-foreground">Volume:</span>{' '}
          <code className="text-xs bg-muted px-1 rounded">{dialog.displayName}</code>
        </div>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="downloadFilename">Filename (optional)</Label>
            <Input
              id="downloadFilename"
              name="downloadFilename"
              placeholder={dialog.displayName || 'download'}
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
          {dialog.serviceName && (
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" name="downloadStopService" className="rounded border-input" />
              Stop service during download
            </label>
          )}
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onClose}>
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
  )
}

function UploadArchiveDialog({ open, dialog, onClose, onSubmit }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            <Upload className="h-4 w-4 inline mr-2" />
            Upload Archive
          </DialogTitle>
          <DialogDescription>Upload and extract an archive into the volume.</DialogDescription>
        </DialogHeader>
        <div className="text-sm text-muted-foreground pb-2">
          <span className="font-medium text-foreground">Volume:</span>{' '}
          <code className="text-xs bg-muted px-1 rounded">{dialog.displayName}</code>
        </div>
        <form onSubmit={onSubmit} className="space-y-4">
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
          {dialog.serviceName && (
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" name="uploadStopService" className="rounded border-input" />
              Stop service during upload
            </label>
          )}
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onClose}>
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
  )
}

export { FORMAT_OPTIONS, DownloadArchiveDialog, UploadArchiveDialog }
