import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { AlertCircle, ArrowUpCircle } from 'lucide-react'

export default function InstallPreviewDialog({ dialog, onClose, onContinue }) {
  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            Install {dialog.name} {dialog.version}
          </DialogTitle>
          <DialogDescription>Review the installation details before proceeding.</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          {dialog.upgrading_from && (
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="gap-1 text-blue-600 border-blue-600">
                <ArrowUpCircle className="h-3 w-3" />
                Upgrading from {dialog.upgrading_from}
              </Badge>
            </div>
          )}
          {dialog.description && (
            <p className="text-sm text-muted-foreground">{dialog.description}</p>
          )}
          <div className="text-sm">
            <span className="text-muted-foreground">Image: </span>
            <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">{dialog.image}</code>
          </div>
          {dialog.volumes?.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-sm font-medium">Volumes</h4>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Mountpoint</TableHead>
                    <TableHead>Quota</TableHead>
                    <TableHead className="text-right"><div className="pr-2">Status</div></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {dialog.volumes.map((vol) => (
                    <TableRow key={vol.name}>
                      <TableCell className="font-mono text-xs">{vol.name}</TableCell>
                      <TableCell className="font-mono text-xs">{vol.mountpoint}</TableCell>
                      <TableCell className="font-mono text-xs">{vol.quota || '-'}</TableCell>
                      <TableCell className="text-right">
                        {vol.migrated ? (
                          <Badge variant="outline" className="text-blue-600 border-blue-600">Migrated</Badge>
                        ) : (
                          <Badge variant="secondary">New</Badge>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
          {dialog.external_ports?.length > 0 && (
            <div className="space-y-1">
              <h4 className="text-sm font-medium">External Ports</h4>
              <div className="text-sm text-muted-foreground">
                {dialog.external_ports.map((p) => (
                  <span key={p.external} className="inline-block mr-3 font-mono text-xs">
                    {p.external} → {p.internal}
                  </span>
                ))}
              </div>
            </div>
          )}
          {dialog.quota_exceeds_disk && (
            <div className="rounded-md border border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20 p-3">
              <p className="text-sm text-yellow-700 dark:text-yellow-400">
                <AlertCircle className="h-4 w-4 inline mr-1" />
                Total volume quotas may exceed available disk space.
              </p>
            </div>
          )}
          {dialog.has_questions && (
            <p className="text-xs text-muted-foreground">
              Configuration questions will follow on the next screen.
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            onClick={() => onContinue(dialog)}
          >
            Continue
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
