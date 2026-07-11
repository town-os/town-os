import { describe, it, expect, vi } from 'vitest'
import { observeBootStatus } from './client-boot.js'

// streamOfFrames builds a ReadableStream that emits a sequence of SSE
// "data: {...}" frames then closes. Used to fake the systemcontroller's
// /boot-status response without hitting the network.
function streamOfFrames(frames) {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const f of frames) {
        controller.enqueue(encoder.encode(`data: ${JSON.stringify(f)}\n\n`))
      }
      controller.close()
    },
  })
}

function okResponse(frames) {
  return { ok: true, status: 200, body: streamOfFrames(frames) }
}

describe('observeBootStatus', () => {
  it('invokes onEvent for each frame and resolves on done', async () => {
    const events = []
    const fetchImpl = vi.fn().mockResolvedValue(okResponse([
      { step: 'open_db' },
      { step: 'reconcile' },
      { done: true },
    ]))

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: (e) => events.push(e),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })

    expect(events).toEqual([
      { step: 'open_db' },
      { step: 'reconcile' },
      { done: true },
    ])
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })

  it('treats a 404 as boot-complete and does not reconnect', async () => {
    // After boot the systemcontroller swaps to the full router, which has no
    // /boot-status route and returns 404. The observer must treat that as
    // done (emit {done:true} and resolve) rather than reconnecting forever on
    // the provisioning screen.
    const events = []
    const onDisconnect = vi.fn()
    const fetchImpl = vi.fn().mockResolvedValue({ ok: false, status: 404, body: null })

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: (e) => events.push(e),
      onDisconnect,
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })

    expect(events).toEqual([{ done: true }])
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(onDisconnect).not.toHaveBeenCalled()
  })

  it('appends a cache-busting query on every fetch', async () => {
    let call = 0
    const fetchImpl = vi.fn().mockImplementation(async () => {
      call += 1
      if (call === 1) throw new Error('net down')
      return okResponse([{ done: true }])
    })

    let nowVal = 1000
    const nowImpl = () => { nowVal += 5; return nowVal }

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: () => {},
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
      nowImpl,
      baseBackoffMs: 1, // don't stall the test
    })

    expect(fetchImpl).toHaveBeenCalledTimes(2)
    for (const [url] of fetchImpl.mock.calls) {
      expect(url).toMatch(/\/boot-status\?t=\d+/)
    }
    // Two different cache-busters because nowImpl advances each call.
    const [u1] = fetchImpl.mock.calls[0]
    const [u2] = fetchImpl.mock.calls[1]
    expect(u1).not.toEqual(u2)
  })

  it('reconnects with exponential backoff until done', async () => {
    const backoffs = []
    let attempt = 0
    const fetchImpl = vi.fn().mockImplementation(() => {
      attempt += 1
      if (attempt < 4) throw new Error('boom')
      return Promise.resolve(okResponse([{ done: true }]))
    })

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: () => {},
      onDisconnect: (ms) => backoffs.push(ms),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
      baseBackoffMs: 2,
      maxBackoffMs: 30,
    })

    // 3 failures → 3 disconnect callbacks with doubling delays: 2,4,8.
    expect(backoffs).toEqual([2, 4, 8])
    expect(fetchImpl).toHaveBeenCalledTimes(4)
  })

  it('caps backoff at maxBackoffMs', async () => {
    const backoffs = []
    let attempt = 0
    const fetchImpl = vi.fn().mockImplementation(() => {
      attempt += 1
      if (attempt < 8) throw new Error('boom')
      return Promise.resolve(okResponse([{ done: true }]))
    })

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: () => {},
      onDisconnect: (ms) => backoffs.push(ms),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
      baseBackoffMs: 1,
      maxBackoffMs: 4,
    })

    // Progression: 1, 2, 4, 4, 4, 4, 4 (capped).
    expect(backoffs).toEqual([1, 2, 4, 4, 4, 4, 4])
  })

  it('gives up when envelope elapses', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new Error('forever down'))
    let t = 0
    // Clock that advances by the sleep delay so the envelope check fires.
    const sleepImpl = (ms) => { t += ms; return Promise.resolve() }
    const nowImpl = () => t

    const start = Date.now()
    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: () => {},
      fetchImpl,
      sleepImpl,
      nowImpl,
      baseBackoffMs: 100,
      maxBackoffMs: 200,
      maxEnvelopeMs: 500,
    })

    // Real wall clock has barely moved — we used virtual time.
    expect(Date.now() - start).toBeLessThan(500)
    expect(fetchImpl).toHaveBeenCalled()
  })

  it('terminates immediately when the abort signal is triggered', async () => {
    const ctrl = new AbortController()
    const fetchImpl = vi.fn().mockImplementation(async () => {
      ctrl.abort()
      throw Object.assign(new Error('aborted'), { name: 'AbortError' })
    })

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: () => {},
      signal: ctrl.signal,
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })

    // Exactly one attempt before the signal short-circuits the loop.
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })

  it('skips malformed lines without breaking the stream', async () => {
    // Raw stream with a bad JSON line mixed among valid frames.
    const stream = new ReadableStream({
      start(controller) {
        const enc = new TextEncoder()
        controller.enqueue(enc.encode('data: {this is not json\n\n'))
        controller.enqueue(enc.encode('data: {"step":"ok"}\n\n'))
        controller.enqueue(enc.encode('data: {"done":true}\n\n'))
        controller.close()
      },
    })
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200, body: stream })
    const got = []
    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: (e) => got.push(e),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })
    expect(got).toEqual([{ step: 'ok' }, { done: true }])
  })
})

// A restart (Refresh Core Services) is the case where a bare 404 lies: the
// process being restarted stays up and serving for a moment after it accepts
// the request, and a booted controller 404s /boot-status whether it is the
// one on its way out or its successor. previousBootID is what tells them
// apart.
describe('observeBootStatus across a controller restart', () => {
  function pingResponse(bootID) {
    return { ok: true, status: 200, json: async () => ({ status: 'ok', boot_id: bootID }) }
  }

  it('does not treat the pre-restart controller 404 as completion', async () => {
    // Every /boot-status probe 404s and every ping still reports the id we
    // captured before requesting the restart: the old process is alive. The
    // observer must keep waiting, not report the refresh finished.
    const events = []
    const onDisconnect = vi.fn()
    const fetchImpl = vi.fn().mockImplementation(async (url) => {
      if (url.includes('/status/ping')) return pingResponse('gen-1')
      return { ok: false, status: 404, body: null }
    })

    await observeBootStatus({
      baseURL: 'http://h',
      previousBootID: 'gen-1',
      onEvent: (e) => events.push(e),
      onDisconnect,
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
      // Two boot-status probes' worth of backoff, then the envelope expires
      // and the loop gives up — proving it never emitted done.
      maxEnvelopeMs: 5000,
      baseBackoffMs: 2000,
    })

    expect(events).toEqual([])
    expect(onDisconnect).toHaveBeenCalled()
  })

  it('reports completion once a 404 comes from a different incarnation', async () => {
    // The successor booted so fast we never caught its stub streaming; the
    // 404 now comes from a controller reporting a new id, so the restart is
    // genuinely done.
    const events = []
    const fetchImpl = vi.fn().mockImplementation(async (url) => {
      if (url.includes('/status/ping')) return pingResponse('gen-2')
      return { ok: false, status: 404, body: null }
    })

    await observeBootStatus({
      baseURL: 'http://h',
      previousBootID: 'gen-1',
      onEvent: (e) => events.push(e),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })

    expect(events).toEqual([{ done: true }])
  })

  it('streams the successor\'s stages once its boot stub answers', async () => {
    // First probe hits the outgoing process (404, same id). It then goes
    // down and the new one comes up serving its boot stub: the stepper gets
    // the restart's real stages, which is the whole point of the dialog.
    const events = []
    let probes = 0
    const fetchImpl = vi.fn().mockImplementation(async (url) => {
      if (url.includes('/status/ping')) return pingResponse('gen-1')
      probes++
      if (probes === 1) return { ok: false, status: 404, body: null }
      return okResponse([{ step: 'open_db' }, { step: 'reconcile' }, { done: true }])
    })

    await observeBootStatus({
      baseURL: 'http://h',
      previousBootID: 'gen-1',
      onEvent: (e) => events.push(e),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })

    expect(events).toEqual([
      { step: 'open_db' },
      { step: 'reconcile' },
      { done: true },
    ])
  })

  it('keeps waiting when the controller reports no boot id at all', async () => {
    // An older controller that predates boot_id cannot prove it is the
    // successor. Waiting (and eventually timing out) is the safe failure:
    // reporting a refresh complete that never happened is not.
    const events = []
    const fetchImpl = vi.fn().mockImplementation(async (url) => {
      if (url.includes('/status/ping')) {
        return { ok: true, status: 200, json: async () => ({ status: 'ok' }) }
      }
      return { ok: false, status: 404, body: null }
    })

    await observeBootStatus({
      baseURL: 'http://h',
      previousBootID: 'gen-1',
      onEvent: (e) => events.push(e),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
      maxEnvelopeMs: 5000,
      baseBackoffMs: 2000,
    })

    expect(events).toEqual([])
  })

  it('still treats a bare 404 as completion on the first-boot screen', async () => {
    // No previousBootID means no restart is being watched: any booted
    // controller answering is the one we were waiting for. Unchanged.
    const events = []
    const fetchImpl = vi.fn().mockResolvedValue({ ok: false, status: 404, body: null })

    await observeBootStatus({
      baseURL: 'http://h',
      onEvent: (e) => events.push(e),
      fetchImpl,
      sleepImpl: () => Promise.resolve(),
    })

    expect(events).toEqual([{ done: true }])
    // No ping probe: without a previous id there is nothing to compare.
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })
})
