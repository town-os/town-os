import { useI18n } from '@/i18n/I18nContext.jsx'
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
  const { t } = useI18n()
  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('volume_reuse.title')}</DialogTitle>
          <DialogDescription>{t('volume_reuse.description')}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <p className="text-sm text-muted-foreground">
            {t('volume_reuse.existing_data', { name: '' })}
            <code className="font-mono text-sm bg-muted px-1 rounded">
              {dialog.name}
            </code>
            {dialog.uninstalledVersions?.length > 0 && (
              <span>
                {' '}{t('volume_reuse.existing_versions', { versions: dialog.uninstalledVersions.join(', ') })}
              </span>
            )}
            {t('volume_reuse.reuse_prompt')}
          </p>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
          >
            {t('volume_reuse.cancel_btn')}
          </Button>
          <Button
            variant="destructive"
            onClick={() => onStartFresh(dialog)}
          >
            {t('volume_reuse.start_fresh_btn')}
          </Button>
          <Button
            onClick={() => onReuse(dialog)}
          >
            {t('volume_reuse.reuse_btn')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
