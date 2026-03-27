/**
 * Reads an SSE progress stream from a fetch Response.
 * Calls onStep(stepKey) for each progress event.
 * Returns when the "done" event is received.
 * Throws on "error" events or stream failures.
 *
 * @param {Response} resp - fetch Response with SSE body
 * @param {(step: string) => void} onStep - callback for each step
 */
export async function readProgressStream(resp, onStep) {
  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop()
    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      try {
        const msg = JSON.parse(line.slice(6))
        if (msg.error) throw new Error(msg.error)
        if (msg.step && onStep) onStep(msg.step)
        if (msg.done) return
      } catch (e) {
        if (e.message && !e.message.startsWith('Unexpected')) throw e
      }
    }
  }
}
