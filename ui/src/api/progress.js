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
      let msg
      try {
        msg = JSON.parse(line.slice(6))
      } catch {
        continue // skip malformed JSON lines
      }
      if (msg.error) throw new Error(msg.error)
      if (msg.step && onStep) onStep(msg.step)
      if (msg.done) return
    }
  }
}
