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
