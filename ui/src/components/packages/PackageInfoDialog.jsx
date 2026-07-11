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
import { Separator } from '@/components/ui/separator'

export default function PackageInfoDialog({ dialog, onClose }) {
  const { t } = useI18n()

  function answerFor(key, question) {
    const value = dialog.responses?.[key]
    if (question.type === 'secret') return t('package_info.secret_mask')
    if (question.type === 'boolean') {
      if (value === undefined || value === '') return '-'
      return ['true', 't', '1'].includes(String(value).trim().toLowerCase())
        ? t('package_info.boolean_true')
        : t('package_info.boolean_false')
    }
    return value || '-'
  }

  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {dialog.name}@{dialog.version}
          </DialogTitle>
          <DialogDescription>{t('package_info.description')}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          {dialog.questions && Object.keys(dialog.questions).length > 0 && (
            <div className="space-y-2">
              <h4 className="text-sm font-medium">{t('package_info.configuration_title')}</h4>
              <div className="space-y-1">
                {Object.entries(dialog.questions).map(([key, question]) => (
                  <div key={key} className="flex justify-between gap-4 text-sm">
                    <span className="text-muted-foreground">{question.query}</span>
                    <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0">
                      {answerFor(key, question)}
                    </code>
                  </div>
                ))}
              </div>
            </div>
          )}
          {dialog.notes && Object.keys(dialog.notes).length > 0 && (
            <>
              {dialog.questions && Object.keys(dialog.questions).length > 0 && (
                <Separator />
              )}
              <div className="space-y-1">
                {Object.entries(dialog.notes).map(([label, value]) => {
                  const noteType = dialog.note_types?.[label]
                  let display
                  if (noteType === 'url') {
                    display = (
                      <a href={value} target="_blank" rel="noopener noreferrer" className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0 underline text-primary">
                        {value}
                      </a>
                    )
                  } else if (noteType === 'email') {
                    display = (
                      <a href={`mailto:${value}`} className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0 underline text-primary">
                        {value}
                      </a>
                    )
                  } else if (noteType === 'phone') {
                    display = (
                      <a href={`tel:${value}`} className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0 underline text-primary">
                        {value}
                      </a>
                    )
                  } else {
                    display = (
                      <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0">
                        {value}
                      </code>
                    )
                  }
                  return (
                    <div key={label} className="flex justify-between gap-4 text-sm">
                      <span className="text-muted-foreground">{label}</span>
                      {display}
                    </div>
                  )
                })}
              </div>
            </>
          )}
          {(!dialog.questions || Object.keys(dialog.questions).length === 0) &&
            (!dialog.notes || Object.keys(dialog.notes).length === 0) && (
            <p className="text-sm text-muted-foreground">{t('package_info.no_config')}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('package_info.close_btn')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
