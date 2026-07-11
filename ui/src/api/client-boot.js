// observeBootStatus streams the systemcontroller's /boot-status SSE
// endpoint, reconnecting automatically while the controller is
// unreachable. It exists so the refresh / system-update UX can keep
// rendering "which step is it on" through the whole restart cycle,
// including a full hardware reboot where the controller may be
// unreachable for 30-60s and the mDNS entry for `*.local` may need
// to age out before the browser re-resolves.
//
// Reconnect policy
// ----------------
// - Base 2s backoff, doubles on each failure, capped at 30s.
// - Total envelope defaults to 5 minutes. After that the function
//   resolves so the caller can show "controller unreachable, please
//   reload" copy.
// - Every reconnect attempt appends a cache-buster query param
//   (?t=<ms>) to defeat intermediate caches AND to coax the browser
//   into re-resolving the hostname rather than stapling to a stale
//   mDNS answer.
//
// Parameters
// ----------
// baseURL          — scheme+host+port of the systemcontroller API.
// onEvent          — called for every SSE frame: `{step?, done?, error?}`.
// onDisconnect     — optional, called when the active stream ends and
//                    a reconnect is about to be scheduled. Receives the
//                    next backoff delay in ms.
// onReconnect      — optional, called each time a reconnect attempt is
//                    issued.
// signal           — AbortSignal; abort to terminate the loop.
// maxEnvelopeMs    — give up after this many ms of total unreachable
//                    time. Default 5 * 60 * 1000.
// baseBackoffMs    — initial reconnect delay. Default 2000.
// maxBackoffMs     — cap on reconnect delay. Default 30000.
// fetchImpl        — injectable fetch for tests. Defaults to global fetch.
// sleepImpl        — injectable sleep for tests. Defaults to setTimeout.
// nowImpl          — injectable clock for tests. Defaults to Date.now.
//
// Returns a Promise that resolves when the stream cleanly ends (either
// a `done:true` frame is observed OR the envelope expires OR the
// signal is aborted). Never throws — UI code can `await` it without a
// try/catch.
export async function observeBootStatus({
  baseURL,
  onEvent,
  onDisconnect,
  onReconnect,
  signal,
  previousBootID = null,
  maxEnvelopeMs = 5 * 60 * 1000,
  baseBackoffMs = 2000,
  maxBackoffMs = 30000,
  fetchImpl = globalThis.fetch.bind(globalThis),
  sleepImpl = (ms, sig) => new Promise((resolve) => {
    const id = setTimeout(resolve, ms)
    if (sig) sig.addEventListener('abort', () => { clearTimeout(id); resolve() }, { once: true })
  }),
  nowImpl = () => Date.now(),
}) {
  const trimmedBase = (baseURL ?? '').replace(/\/+$/, '')
  const startedAt = nowImpl()
  let backoff = baseBackoffMs
  let lastConnectedAt = startedAt
  let done = false

  while (!done) {
    if (signal?.aborted) return
    if (nowImpl() - lastConnectedAt > maxEnvelopeMs) return

    try {
      if (onReconnect) onReconnect()
      const cacheBust = nowImpl()
      const resp = await fetchImpl(`${trimmedBase}/boot-status?t=${cacheBust}`, {
        signal,
        cache: 'no-store',
        credentials: 'same-origin',
      })

      if (resp.status === 404) {
        // A 404 means we reached a *fully booted* controller: once boot
        // completes the systemcontroller swaps its root handler from the
        // boot-status stub (which always serves this route) to the full
        // Echo router (which has no such route).
        //
        // Which booted controller, though? On the first-boot screen there
        // is only one answer — boot finished, we're done. But during a
        // Refresh Core Services the OLD process is still up and serving
        // for about a second after it accepts the request, and it 404s
        // this route exactly like a finished new process would. Treating
        // that as completion is what made the stepper flash straight to
        // "ready" and then sit there: it had latched onto the outgoing
        // process and never watched the incoming one boot at all.
        //
        // So when the caller hands us the boot id it captured before
        // asking for the restart, a 404 only means "done" if the
        // controller now answering reports a DIFFERENT id (the successor
        // booted so fast we never caught its stub). A matching id is the
        // pre-restart process still on its feet — reconnect and wait for
        // it to go down.
        if (!previousBootID) {
          if (onEvent) onEvent({ done: true })
          return
        }
        const currentBootID = await readBootID(fetchImpl, trimmedBase, signal, nowImpl)
        if (currentBootID && currentBootID !== previousBootID) {
          if (onEvent) onEvent({ done: true })
          return
        }
        throw new Error('boot-status 404 from the pre-restart controller')
      }

      if (!resp.ok || !resp.body) {
        // Other 4xx/5xx during boot means we hit the early handler *before*
        // it was even ready, OR the controller is up but /boot-status
        // is temporarily not routable. Treat both as reconnectable.
        throw new Error(`boot-status HTTP ${resp.status}`)
      }

      lastConnectedAt = nowImpl()
      backoff = baseBackoffMs // healthy connection → reset backoff

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { done: readerDone, value } = await reader.read()
        if (readerDone) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          let evt
          try {
            evt = JSON.parse(line.slice(6))
          } catch {
            continue // malformed line, skip
          }
          if (onEvent) onEvent(evt)
          if (evt.done) {
            done = true
            break
          }
        }
        if (done) break
      }
    } catch (err) {
      if (signal?.aborted) return
      // fallthrough to reconnect
      void err
    }

    if (done) break

    if (onDisconnect) onDisconnect(backoff)

    if (nowImpl() - startedAt + backoff > maxEnvelopeMs) return

    await sleepImpl(backoff, signal)
    backoff = Math.min(backoff * 2, maxBackoffMs)
  }
}

/**
 * Read the `boot_id` the controller currently answering /status/ping
 * reports, or null if it cannot be read.
 *
 * Both the boot stub and the full router carry the field, so this works
 * on either side of the handler swap. The stub answers 503 while booting
 * (deliberately, so readiness probes don't call a half-booted controller
 * ready), so the status code is ignored and only the body is parsed.
 *
 * Returns null — never throws — when the controller is unreachable, the
 * body isn't JSON, or the field is absent (an older controller). A null
 * is treated by the caller as "cannot prove the successor is up", which
 * keeps it waiting rather than falsely reporting the refresh complete.
 */
export async function readBootID(fetchImpl, baseURL, signal, nowImpl = () => Date.now()) {
  try {
    const resp = await fetchImpl(`${baseURL}/status/ping?t=${nowImpl()}`, {
      signal,
      cache: 'no-store',
      credentials: 'same-origin',
    })
    const body = await resp.json()
    return typeof body?.boot_id === 'string' && body.boot_id ? body.boot_id : null
  } catch {
    return null
  }
}
