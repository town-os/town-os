import { useEffect, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import getClient from '@/lib/client-instance.js'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { X } from 'lucide-react'

// Boolean answers travel as strings. Cached responses are already canonical,
// but a package's YAML default may use any spelling Go's strconv.ParseBool
// accepts, which is what the backend validates against.
function parseBoolean(value) {
  return ['true', 't', '1'].includes(String(value ?? '').trim().toLowerCase())
}

export default function InstallQuestionsDialog({ dialog, onClose, onSubmit, onClearField }) {
  const { t } = useI18n()
  const [networks, setNetworks] = useState([])
  const [booleans, setBooleans] = useState({})

  useEffect(() => {
    if (!dialog.open) return
    const initial = {}
    for (const [key, question] of Object.entries(dialog.questions || {})) {
      if (question.type !== 'boolean') continue
      const cached = dialog.responses?.[key]
      initial[key] = parseBoolean(cached === undefined || cached === '' ? question.default : cached)
    }
    setBooleans(initial)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dialog.open])

  useEffect(() => {
    if (!dialog.open) return
    const client = getClient()
    if (typeof client.listNetworks !== 'function') return
    let cancelled = false
    client
      .listNetworks()
      .then((list) => {
        if (!cancelled) setNetworks((list || []).filter((n) => n.enabled))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [dialog.open])

  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>
            {t('install_questions.title', { name: dialog.name, version: dialog.version })}
          </DialogTitle>
          <DialogDescription>{t('install_questions.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="__network__">{t('networks.install_label')}</Label>
              <select
                id="__network__"
                name="__network__"
                defaultValue="home"
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
              >
                {(networks.length ? networks : [{ name: 'home' }]).map((n) => (
                  <option key={n.name} value={n.name}>
                    {n.name}
                  </option>
                ))}
              </select>
              <p className="text-xs text-muted-foreground">{t('networks.install_help')}</p>
            </div>
            {dialog.questions &&
              Object.entries(dialog.questions).map(
                ([key, question]) => {
                  const fieldError = dialog.fieldErrors?.[key]
                  const cachedValue = dialog.responses?.[key]
                  const isCleared = dialog.clearedFields?.[key]
                  const hasCachedValue = !!cachedValue && !isCleared

                  if (question.type === 'boolean') {
                    const checked = !!booleans[key]
                    // The checkbox itself carries no name; the hidden input is
                    // the form field, so the submit handler reads "true"/"false"
                    // rather than a checkbox's "on"/absent value.
                    return (
                      <div key={key} className="space-y-2">
                        <div className="flex items-center gap-2">
                          <input
                            id={`${key}__toggle`}
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setBooleans((prev) => ({ ...prev, [key]: e.target.checked }))
                            }
                            className="h-4 w-4 rounded border border-input accent-primary"
                          />
                          <Label htmlFor={`${key}__toggle`}>{question.query}</Label>
                        </div>
                        <input type="hidden" name={key} value={checked ? 'true' : 'false'} />
                        {fieldError && <p className="text-sm text-destructive">{fieldError}</p>}
                      </div>
                    )
                  }

                  // Build placeholder: show default or type hint
                  let placeholder
                  if (question.default) {
                    placeholder = t('install_questions.default_hint', { value: question.default })
                  } else if (question.type === 'duration') {
                    placeholder = t('install_questions.placeholder_duration')
                  } else if (question.type === 'port') {
                    placeholder = t('install_questions.placeholder_port')
                  } else if (question.type === 'hostname') {
                    placeholder = t('install_questions.placeholder_hostname')
                  }

                  return (
                    <div key={key} className="space-y-2">
                      <Label htmlFor={key}>{question.query}</Label>
                      {hasCachedValue ? (
                        <div className="flex items-center gap-2">
                          <div className="flex-1 rounded-md border bg-muted/50 px-3 py-2 text-sm font-mono">
                            {question.type === 'secret' ? t('install_questions.secret_mask') : cachedValue}
                          </div>
                          <input type="hidden" name={key} value={cachedValue} />
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                                onClick={() => onClearField(key)}
                              >
                                <X className="h-4 w-4" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>{t('install_questions.clear_tooltip')}</TooltipContent>
                          </Tooltip>
                        </div>
                      ) : (
                        <Input
                          id={key}
                          name={key}
                          type={question.type === 'secret' ? 'password' : 'text'}
                          placeholder={placeholder}
                          defaultValue=""
                          className={fieldError ? 'border-destructive' : ''}
                        />
                      )}
                      {question.default && !hasCachedValue && (
                        <p className="text-xs text-muted-foreground">
                          {t('install_questions.default_prefix')}<span className="font-mono">{question.default}</span>
                        </p>
                      )}
                      {question.type === 'duration' && (
                        <p className="text-xs text-muted-foreground">
                          {t('install_questions.duration_hint')}
                        </p>
                      )}
                      {fieldError && (
                        <p className="text-sm text-destructive">{fieldError}</p>
                      )}
                    </div>
                  )
                },
              )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              type="button"
              onClick={onClose}
            >
              {t('install_questions.cancel_btn')}
            </Button>
            <Button type="submit">{t('install_questions.install_btn')}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
