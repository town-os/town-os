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

      if (!resp.ok || !resp.body) {
        // 4xx/5xx during boot means we hit the early handler *before*
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
