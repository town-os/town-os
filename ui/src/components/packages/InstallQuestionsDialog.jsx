import { useEffect, useRef, useState } from 'react'
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
import { X, RotateCw, Check } from 'lucide-react'

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
  // An oauth question is answered by a device flow rather than by typing: state
  // per question key is { status, token, approveUrl, userCode, error }.
  const [oauth, setOauth] = useState({})
  const oauthCancelled = useRef(false)

  useEffect(() => {
    // A dialog that closes mid-flow must not leave a poll loop running against a
    // flow nobody is waiting for.
    oauthCancelled.current = !dialog.open
  }, [dialog.open])

  async function runOAuth(key) {
    setOauth((prev) => ({ ...prev, [key]: { status: 'starting' } }))
    try {
      const started = await getClient().startOAuth(dialog.repo, dialog.name, dialog.version, key)
      setOauth((prev) => ({
        ...prev,
        [key]: {
          status: 'waiting',
          approveUrl: started.approve_url,
          userCode: started.user_code || '',
        },
      }))
      // Opened from the click handler's own task so the browser still counts it
      // as user-initiated; opening it after the await would be a popup block.
      window.open(started.approve_url, '_blank', 'noopener,noreferrer')

      const interval = Math.max(1000, started.interval_ms || 5000)
      for (;;) {
        if (oauthCancelled.current) return
        await new Promise((resolve) => setTimeout(resolve, interval))
        if (oauthCancelled.current) return
        const polled = await getClient().pollOAuth(started.flow_id)
        if (polled.status === 'approved') {
          setOauth((prev) => ({ ...prev, [key]: { status: 'approved', token: polled.token } }))
          return
        }
        if (polled.status === 'expired') {
          setOauth((prev) => ({
            ...prev,
            [key]: { status: 'error', error: t('install_questions.oauth_expired') },
          }))
          return
        }
      }
    } catch (err) {
      setOauth((prev) => ({
        ...prev,
        [key]: { status: 'error', error: err.detail || err.message },
      }))
    }
  }

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

                  if (question.type === 'oauth') {
                    const flow = oauth[key] || {}
                    // A cached token from a previous install is already an answer:
                    // the flow does not have to be run again to reinstall.
                    const token = flow.token || (hasCachedValue ? cachedValue : '')
                    const connected = !!token
                    return (
                      <div key={key} className="space-y-2">
                        <Label>{question.query}</Label>
                        {/* The token is the form's value whether it came from the
                            flow just now or from the previous install. */}
                        <input type="hidden" name={key} value={token} />
                        <div className="flex items-center gap-2">
                          <Button
                            type="button"
                            variant={connected ? 'outline' : 'default'}
                            size="sm"
                            disabled={flow.status === 'starting' || flow.status === 'waiting'}
                            onClick={() => runOAuth(key)}
                          >
                            {connected
                              ? t('install_questions.oauth_reconnect')
                              : t('install_questions.oauth_connect')}
                          </Button>
                          {connected && (
                            <span className="flex items-center gap-1 text-sm text-green-600">
                              <Check className="h-4 w-4" />
                              {t('install_questions.oauth_connected')}
                            </span>
                          )}
                          {flow.status === 'starting' && (
                            <span className="text-sm text-muted-foreground animate-pulse">
                              {t('install_questions.oauth_starting')}
                            </span>
                          )}
                          {flow.status === 'waiting' && (
                            <span className="text-sm text-muted-foreground animate-pulse">
                              {t('install_questions.oauth_waiting')}
                            </span>
                          )}
                        </div>
                        {flow.status === 'waiting' && flow.userCode && (
                          <p className="text-sm">
                            {t('install_questions.oauth_user_code')}{' '}
                            <span className="font-mono font-medium">{flow.userCode}</span>
                          </p>
                        )}
                        {/* The approval page opens in a new tab, which a popup
                            blocker may swallow. Leave the link on screen so the
                            flow is not simply stuck when it does. */}
                        {flow.status === 'waiting' && flow.approveUrl && (
                          <a
                            href={flow.approveUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="block text-sm underline text-primary break-all"
                          >
                            {t('install_questions.oauth_open_manually')}
                          </a>
                        )}
                        {flow.status === 'error' && (
                          <p className="text-sm text-destructive">{flow.error}</p>
                        )}
                        {fieldError && <p className="text-sm text-destructive">{fieldError}</p>}
                      </div>
                    )
                  }

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

                  const isSecret = question.type === 'secret'

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
                  } else if (isSecret) {
                    placeholder = t('install_questions.placeholder_secret')
                  } else if (question.optional) {
                    // An optional question with no default: say so, or the empty
                    // field reads as one the user forgot to fill in.
                    placeholder = t('install_questions.placeholder_optional')
                  }

                  return (
                    <div key={key} className="space-y-2">
                      <Label htmlFor={key}>
                        {question.query}
                        {question.optional && (
                          <span className="ml-1 text-xs font-normal text-muted-foreground">
                            {t('install_questions.optional_suffix')}
                          </span>
                        )}
                      </Label>
                      {hasCachedValue ? (
                        <div className="flex items-center gap-2">
                          <div className="flex-1 rounded-md border bg-muted/50 px-3 py-2 text-sm font-mono break-all">
                            {cachedValue}
                          </div>
                          <input type="hidden" name={key} value={cachedValue} />
                          {/* A secret is not discarded so much as rotated: clearing
                              it hands an empty response to the server, which mints a
                              fresh one. Say that, rather than showing a delete X. */}
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                aria-label={
                                  isSecret
                                    ? t('install_questions.recycle_label')
                                    : t('install_questions.clear_label')
                                }
                                className={
                                  isSecret
                                    ? 'h-8 w-8 p-0 text-muted-foreground hover:text-primary'
                                    : 'h-8 w-8 p-0 text-muted-foreground hover:text-destructive'
                                }
                                onClick={() => onClearField(key)}
                              >
                                {isSecret ? <RotateCw className="h-4 w-4" /> : <X className="h-4 w-4" />}
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                              {isSecret
                                ? t('install_questions.recycle_tooltip')
                                : t('install_questions.clear_tooltip')}
                            </TooltipContent>
                          </Tooltip>
                        </div>
                      ) : (
                        <Input
                          id={key}
                          name={key}
                          type="text"
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
