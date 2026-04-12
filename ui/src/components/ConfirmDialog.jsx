import { useI18n } from '@/i18n/I18nContext.jsx'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Loader2 } from 'lucide-react'

export default function ConfirmDialog({
  open,
  title,
  children,
  onConfirm,
  onCancel,
  confirmLabel,
  cancelLabel,
  variant = 'default',
  loading = false,
}) {
  const { t } = useI18n()
  return (
    <Dialog open={!!open} onOpenChange={(v) => !v && !loading && onCancel?.()}>
      <DialogContent onPointerDownOutside={loading ? (e) => e.preventDefault() : undefined}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{children}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={loading}>
            {cancelLabel || t('confirm.default_cancel_label')}
          </Button>
          <Button
            variant={variant === 'destructive' ? 'destructive' : 'default'}
            onClick={onConfirm}
            disabled={loading}
          >
            {loading && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {confirmLabel || t('confirm.default_confirm_label')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
