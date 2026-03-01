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
import { Badge } from '@/components/ui/badge'
import { Pencil } from 'lucide-react'

function VolumeModifyDialog({ open, dialog, onClose, onSubmit }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            <Pencil className="h-4 w-4 inline mr-2" />
            Modify Volume
          </DialogTitle>
          <DialogDescription>Change the name or quota of this volume.</DialogDescription>
        </DialogHeader>
        <div className="space-y-1 text-sm text-muted-foreground pb-2">
          <div><span className="font-medium text-foreground">Volume:</span> {dialog.displayName}</div>
          <div><span className="font-medium text-foreground">State:</span>{' '}
            <Badge variant={dialog.state === 'installed' ? 'default' : 'secondary'}>
              {dialog.state}
            </Badge>
          </div>
          {dialog.serviceName && (
            <div><span className="font-medium text-foreground">Service:</span> <code className="text-xs bg-muted px-1 rounded">{dialog.serviceName}</code></div>
          )}
        </div>
        <form onSubmit={onSubmit} className="space-y-4">
          {dialog.state === 'user' && (
            <div className="space-y-2">
              <Label htmlFor="modifyName">Name</Label>
              <Input
                id="modifyName"
                name="modifyName"
                defaultValue={dialog.displayName || ''}
              />
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="modifyQuota">Quota (0 = unlimited)</Label>
            <div className="flex gap-2">
              <Input
                id="modifyQuota"
                name="modifyQuota"
                type="number"
                min="0"
                step="any"
                defaultValue={dialog.quotaValue || ''}
                placeholder="0"
                className="flex-1"
              />
              <select
                name="modifyQuotaUnit"
                defaultValue={dialog.quotaUnit || 'GB'}
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
            <Button variant="outline" type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit">Save Changes</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export default VolumeModifyDialog
