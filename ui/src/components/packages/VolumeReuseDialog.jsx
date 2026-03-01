import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

export default function VolumeReuseDialog({ dialog, onClose, onStartFresh, onReuse }) {
  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Existing Data Found</DialogTitle>
          <DialogDescription>Choose whether to reuse existing volume data or start fresh.</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <p className="text-sm text-muted-foreground">
            Previous data exists for{' '}
            <code className="font-mono text-sm bg-muted px-1 rounded">
              {dialog.name}
            </code>
            {dialog.uninstalledVersions?.length > 0 && (
              <span>
                {' '}(versions: {dialog.uninstalledVersions.join(', ')})
              </span>
            )}
            . Would you like to reuse it or start fresh?
          </p>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={() => onStartFresh(dialog)}
          >
            Start Fresh
          </Button>
          <Button
            onClick={() => onReuse(dialog)}
          >
            Reuse Data
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
