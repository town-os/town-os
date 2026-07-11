import { useI18n } from '@/i18n/I18nContext.jsx'
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
  const { t } = useI18n()
  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>
            {t('install_preview.title', { name: dialog.name, version: dialog.version })}
          </DialogTitle>
          <DialogDescription>{t('install_preview.description')}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          {dialog.upgrading_from && (
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="gap-1 text-blue-600 border-blue-600">
                <ArrowUpCircle className="h-3 w-3" />
                {t('install_preview.upgrading_from', { version: dialog.upgrading_from })}
              </Badge>
            </div>
          )}
          {dialog.description && (
            <p className="text-sm text-muted-foreground">{dialog.description}</p>
          )}
          <div className="text-sm">
            <span className="text-muted-foreground">
              {dialog.runtime === 'vm' ? t('install_preview.vm_image_label') : t('install_preview.image_label')}{' '}
            </span>
            <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">
              {dialog.runtime === 'vm' && dialog.vm ? dialog.vm.image : dialog.image}
            </code>
          </div>
          {dialog.runtime === 'vm' && dialog.vm && (
            <div className="flex gap-4 text-sm">
              {dialog.vm.memory && (
                <span>
                  <span className="text-muted-foreground">{t('install_preview.vm_memory_label')} </span>
                  <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">{dialog.vm.memory}</code>
                </span>
              )}
              {dialog.vm.cpus > 0 && (
                <span>
                  <span className="text-muted-foreground">{t('install_preview.vm_cpus_label')} </span>
                  <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">{dialog.vm.cpus}</code>
                </span>
              )}
            </div>
          )}
          {dialog.volumes?.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-sm font-medium">{t('install_preview.volumes_title')}</h4>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('install_preview.col_name')}</TableHead>
                    <TableHead>{t('install_preview.col_mountpoint')}</TableHead>
                    <TableHead>{t('install_preview.col_quota')}</TableHead>
                    <TableHead className="text-right"><div className="pr-2">{t('install_preview.col_status')}</div></TableHead>
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
                          <Badge variant="outline" className="text-blue-600 border-blue-600">{t('install_preview.status_migrated')}</Badge>
                        ) : (
                          <Badge variant="secondary">{t('install_preview.status_new')}</Badge>
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
              <h4 className="text-sm font-medium">{t('install_preview.external_ports_title')}</h4>
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
                {t('install_preview.quota_exceeds_disk')}
              </p>
            </div>
          )}
          {dialog.has_questions && (
            <p className="text-xs text-muted-foreground">
              {t('install_preview.has_questions_hint')}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
          >
            {t('install_preview.cancel_btn')}
          </Button>
          <Button
            onClick={() => onContinue(dialog)}
          >
            {t('install_preview.continue_btn')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
