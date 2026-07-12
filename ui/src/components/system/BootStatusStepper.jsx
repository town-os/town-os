import { useEffect, useRef, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { CheckCircle2, Circle, Loader2, AlertTriangle } from 'lucide-react'
import { observeBootStatus } from '@/api/client-boot.js'
import { bootSteps } from './boot-steps.js'

// PACKAGE_PREFIX matches PackageStepPrefix in
// src/svc/systemcontroller/boot_status.go. The freshness stage emits one
// "restarting_<repo>/<name>" event per installed package; each becomes its
// own row under the restart_packages stage.
const PACKAGE_PREFIX = 'restarting_'
const RESTART_STAGE = 'restart_packages'
const MDNS_HINT_DELAY_MS = 60_000

/**
 * Render a vertical stepper driven by a live /boot-status SSE stream.
 *
 * Props:
 *   baseURL      — systemcontroller URL passed to observeBootStatus.
 *   onComplete   — called once when the stream emits `{done:true}`.
 *   onError      — called if the stream reports an error frame.
 *   hostname     — hostname shown in the mDNS reconnect hint. Falls
 *                  back to window.location.hostname.
 *   previousBootID — boot id captured before a restart was requested.
 *                  Pass it when the stepper is watching a controller
 *                  RESTART (Refresh Core Services) rather than a
 *                  first boot: it stops the still-running outgoing
 *                  process from being mistaken for a finished new one.
 *                  Omit on the first-boot screen, where any booted
 *                  controller answering is by definition the one we
 *                  were waiting for.
 *
 * The stepper owns the subscription lifecycle: on mount it starts
 * observing; on unmount it aborts. It also surfaces a "reconnecting
 * in Ns" indicator while the stream is disconnected and a secondary
 * mDNS-cache hint after MDNS_HINT_DELAY_MS of total disconnection
 * (users on *.local hostnames sometimes see their browser staple to
 * a stale address after a full hardware reboot — the only safe
 * remedy from JS is reminding the user to reload).
 */
export default function BootStatusStepper({ baseURL, onComplete, onError, hostname, previousBootID = null }) {
  const { t } = useI18n()
  const [currentStep, setCurrentStep] = useState(null)
  // packages accumulates in arrival order — the backend restarts them
  // serially, so the most recent one is always the in-progress row and
  // every earlier one is done.
  const [packages, setPackages] = useState([])
  const [currentPackage, setCurrentPackage] = useState(null)
  const [errorMsg, setErrorMsg] = useState(null)
  const [reconnectIn, setReconnectIn] = useState(null) // ms until retry, or null when connected
  const [showMdnsHint, setShowMdnsHint] = useState(false)
  const mdnsTimerRef = useRef(null)

  useEffect(() => {
    const ctrl = new AbortController()

    observeBootStatus({
      baseURL,
      previousBootID,
      signal: ctrl.signal,
      onEvent: (evt) => {
        if (evt.error) {
          setErrorMsg(evt.error)
          if (onError) onError(evt.error)
          return
        }
        if (evt.done) {
          setCurrentStep('ready')
          setCurrentPackage(null)
          if (onComplete) onComplete()
          return
        }
        if (typeof evt.step === 'string') {
          // Connected, cancel the "reconnecting" indicator.
          setReconnectIn(null)
          if (mdnsTimerRef.current) {
            clearTimeout(mdnsTimerRef.current)
            mdnsTimerRef.current = null
          }
          setShowMdnsHint(false)

          if (evt.step.startsWith(PACKAGE_PREFIX)) {
            const name = evt.step.slice(PACKAGE_PREFIX.length)
            // Append on first sight. History replay after a reconnect
            // re-delivers earlier package events, so dedupe rather than
            // growing a duplicate row per reconnect.
            setPackages((prev) => (prev.includes(name) ? prev : [...prev, name]))
            setCurrentPackage(name)
            setCurrentStep(RESTART_STAGE)
          } else if (bootSteps.includes(evt.step)) {
            setCurrentStep(evt.step)
          }
          // A step we don't know about (e.g. one the backend added but
          // this build's bootSteps doesn't list yet) is deliberately
          // ignored rather than applied: applying it would resolve to
          // indexOf === -1 in stageState and reset every completed row
          // back to "pending". Ignoring it keeps progress monotonic — the
          // next known step advances the stepper. boot-steps.js is kept in
          // sync with the backend so this is only a defensive backstop.
        }
      },
      onDisconnect: (ms) => {
        setReconnectIn(ms)
        if (!mdnsTimerRef.current) {
          mdnsTimerRef.current = setTimeout(() => setShowMdnsHint(true), MDNS_HINT_DELAY_MS)
        }
      },
    })

    return () => {
      ctrl.abort()
      if (mdnsTimerRef.current) {
        clearTimeout(mdnsTimerRef.current)
        mdnsTimerRef.current = null
      }
    }
    // baseURL/callbacks are stable in practice; intentionally exclude
    // to avoid resubscribing on every parent re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [baseURL, previousBootID])

  const currentIndex = currentStep ? bootSteps.indexOf(currentStep) : -1
  const hasError = Boolean(errorMsg)

  // Flatten the five stages and the per-package rows into one list so the
  // packages are rendered as peers of the stages rather than as a nested
  // sub-list: each installed package is a first-class row with its own
  // icon and state.
  const rows = []
  for (const step of bootSteps) {
    rows.push({
      key: step,
      step,
      label: t(`boot.label.${step}`),
      state: stageState(bootSteps.indexOf(step), currentIndex, hasError),
    })
    if (step === RESTART_STAGE) {
      for (const name of packages) {
        rows.push({
          key: `${PACKAGE_PREFIX}${name}`,
          step: `${PACKAGE_PREFIX}${name}`,
          pkg: name,
          label: t('boot.label.restarting_pkg', { name }),
          state: packageState(name, packages, currentPackage, currentIndex, hasError),
        })
      }
    }
  }

  return (
    <div className="space-y-3" data-testid="boot-status-stepper">
      <ol className="space-y-1" aria-label="boot-progress">
        {rows.map((row) => (
          <li
            key={row.key}
            className="flex items-center gap-2 text-sm"
            data-step={row.step}
            data-state={row.state}
            {...(row.pkg ? { 'data-package': row.pkg } : {})}
          >
            <StepIcon state={row.state} />
            <span className={row.state === 'pending' ? 'text-muted-foreground' : ''}>{row.label}</span>
          </li>
        ))}
      </ol>

      {reconnectIn !== null && !errorMsg && (
        <div
          className="flex items-center gap-2 rounded-md bg-muted p-3 text-sm text-muted-foreground"
          data-testid="boot-reconnect-banner"
        >
          <Loader2 className="h-4 w-4 animate-spin" />
          {t('boot.label.reconnecting', { seconds: Math.ceil(reconnectIn / 1000) })}
        </div>
      )}

      {showMdnsHint && !errorMsg && (
        <div
          className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-950"
          data-testid="boot-mdns-hint"
        >
          <AlertTriangle className="mt-0.5 h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0" />
          <span>{t('boot.label.mdns_hint', { hostname: hostname || globalThis.location?.hostname || 'host' })}</span>
        </div>
      )}

      {errorMsg && (
        <div
          className="flex items-start gap-2 rounded-md border border-destructive bg-destructive/10 p-3 text-sm"
          data-testid="boot-error-banner"
        >
          <AlertTriangle className="mt-0.5 h-4 w-4 text-destructive shrink-0" />
          <span>{errorMsg}</span>
        </div>
      )}
    </div>
  )
}

// stageState classifies one of the five coarse stages by comparing its
// index against the current stage's index.
function stageState(idx, currentIndex, hasError) {
  if (hasError && idx === currentIndex) return 'error'
  if (currentIndex < 0) return 'pending'
  if (idx < currentIndex) return 'done'
  if (idx === currentIndex) return 'in_progress'
  return 'pending'
}

// packageState classifies one per-package row. Packages only have meaning
// while the restart stage is current: before it, they cannot have been
// reached; after it, every one of them is finished (the backend restarts
// them serially and only advances past the stage once the loop ends).
function packageState(name, packages, currentPackage, currentIndex, hasError) {
  const restartIndex = bootSteps.indexOf(RESTART_STAGE)
  if (currentIndex < restartIndex) return 'pending'
  if (currentIndex > restartIndex) return 'done'

  const idx = packages.indexOf(name)
  const curIdx = currentPackage ? packages.indexOf(currentPackage) : -1
  if (idx < curIdx) return 'done'
  if (idx === curIdx) return hasError ? 'error' : 'in_progress'
  return 'pending'
}

function StepIcon({ state }) {
  switch (state) {
    case 'done':
      return <CheckCircle2 className="h-4 w-4 text-green-600 shrink-0" aria-label="done" />
    case 'in_progress':
      return <Loader2 className="h-4 w-4 text-primary animate-spin shrink-0" aria-label="in-progress" />
    case 'error':
      return <AlertTriangle className="h-4 w-4 text-destructive shrink-0" aria-label="error" />
    default:
      return <Circle className="h-4 w-4 text-muted-foreground shrink-0" aria-label="pending" />
  }
}
