import { useRef, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { toast } from 'sonner'
import { Copy, Check } from 'lucide-react'
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/utils.js'

// SecretAnswer shows a secret response masked, and copies the real value to the
// clipboard on click so the user can paste it without ever putting it on screen.
// The copy icon and the hover/focus styling are what tell the user the mask is
// clickable at all -- without them it reads as inert text.
function SecretAnswer({ value, t }) {
  const [copied, setCopied] = useState(false)
  const btnRef = useRef(null)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          ref={btnRef}
          type="button"
          aria-label={t('package_info.secret_copy_label')}
          className="inline-flex items-center gap-1.5 font-mono text-xs bg-muted hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none px-1.5 py-0.5 rounded shrink-0 cursor-pointer transition-colors"
          onClick={async () => {
            try {
              // The insecure-context fallback needs to mount its scratch
              // textarea inside the dialog: on document.body it lands outside
              // this dialog's focus scope, which pulls focus back and drops the
              // selection, so the copy silently yields the wrong text.
              await copyToClipboard(value, btnRef.current?.parentElement ?? undefined)
              setCopied(true)
              setTimeout(() => setCopied(false), 2000)
              toast.success(t('package_info.secret_copied'))
            } catch {
              toast.error(t('package_info.secret_copy_failed'))
            }
          }}
        >
          {t('package_info.secret_mask')}
          {copied ? (
            <Check className="h-3 w-3 text-green-600" />
          ) : (
            <Copy className="h-3 w-3 text-muted-foreground" />
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent>{t('package_info.secret_copy_tooltip')}</TooltipContent>
    </Tooltip>
  )
}

export default function PackageInfoDialog({ dialog, onClose }) {
  const { t } = useI18n()

  function answerFor(key, question) {
    const value = dialog.responses?.[key]
    // An oauth answer is the token the device flow returned -- a credential, so
    // it is masked and copyable exactly like a secret rather than printed.
    if (question.type === 'secret' || question.type === 'oauth') {
      if (value === undefined || value === '') return '-'
      return <SecretAnswer value={String(value)} t={t} />
    }
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
                {Object.entries(dialog.questions).map(([key, question]) => {
                  const answer = answerFor(key, question)
                  return (
                    <div key={key} className="flex justify-between gap-4 text-sm">
                      <span className="text-muted-foreground">{question.query}</span>
                      {typeof answer === 'string' ? (
                        <code className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded shrink-0">
                          {answer}
                        </code>
                      ) : (
                        answer
                      )}
                    </div>
                  )
                })}
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
