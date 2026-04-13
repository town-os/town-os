import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import JournalViewer from './JournalViewer.jsx'
import { I18nProvider } from '@/i18n/I18nContext.jsx'

const baseTs = new Date('2024-01-01T12:00:00Z').getTime()
const sampleEntries = Array.from({ length: 20 }, (_, i) => ({
  Cursor: `c${i}`,
  RealtimeTimestamp: baseTs + i * 1000,
  Message: `log line ${i}`,
}))

const mockLogTail = vi.fn()

vi.mock('@/lib/client-instance.js', () => ({
  default: () => ({ logTail: mockLogTail }),
}))

function renderViewer() {
  return render(
    <I18nProvider>
      <JournalViewer
        journalUnit="test.service"
        onClose={() => {}}
        units={[{ Name: 'test.service', ActiveState: 'active', SubState: 'running' }]}
      />
    </I18nProvider>,
  )
}

describe('JournalViewer', () => {
  let origScrollHeight
  let origScrollTop
  let scrollTopValue

  beforeEach(() => {
    mockLogTail.mockReset()
    mockLogTail.mockResolvedValue({
      entries: sampleEntries,
      cursor: '',
      end_cursor: 'end',
    })

    // jsdom has no layout, so scrollHeight is always 0 and scrollTop assignments
    // don't stick. Mock both on HTMLElement so the scroll-to-bottom effect has
    // a non-zero target and we can observe what it set.
    scrollTopValue = 0
    origScrollHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollHeight')
    origScrollTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollTop')
    Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
      configurable: true,
      // Return a non-zero value only once the log lines have been committed
      // to the DOM. This lets us distinguish "scrolled before entries arrived"
      // (bug) from "scrolled after entries rendered" (fix).
      get() {
        return document.body.textContent.includes('log line 19') ? 5000 : 100
      },
    })
    Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
      configurable: true,
      get() { return scrollTopValue },
      set(v) { scrollTopValue = v },
    })
  })

  afterEach(() => {
    cleanup()
    if (origScrollHeight) Object.defineProperty(HTMLElement.prototype, 'scrollHeight', origScrollHeight)
    else delete HTMLElement.prototype.scrollHeight
    if (origScrollTop) Object.defineProperty(HTMLElement.prototype, 'scrollTop', origScrollTop)
    else delete HTMLElement.prototype.scrollTop
  })

  it('renders minute groups expanded by default', async () => {
    renderViewer()
    // Every entry must be visible on first render — not just the group preview.
    // All 20 entries share the same minute bucket, so if groups defaulted to
    // collapsed only "log line 0" (the preview) would appear.
    await screen.findByText(/log line 0/)
    expect(screen.getByText(/log line 19/)).toBeTruthy()
  })

  it('scrolls to bottom once entries have loaded', async () => {
    renderViewer()
    // Wait until entries are in the DOM. By the time the final scroll-to-bottom
    // effect runs, scrollHeight returns 5000 (entries present) rather than 100.
    await screen.findByText(/log line 19/)
    await waitFor(() => {
      expect(scrollTopValue).toBe(5000)
    })
  })

  it('does not fire scroll-to-bottom before entries arrive', async () => {
    // Delay the logTail response so we can observe state between render and
    // entries landing. If the guard is misconfigured (the original bug), the
    // effect fires on the empty first render and scrollTop gets pinned to 100
    // (the empty-content scrollHeight) and never advances.
    let resolveLoad
    mockLogTail.mockImplementationOnce(
      () => new Promise((resolve) => { resolveLoad = resolve }),
    )

    renderViewer()

    // Give React a tick to flush the initial effects.
    await Promise.resolve()
    expect(scrollTopValue).toBe(0)

    resolveLoad({ entries: sampleEntries, cursor: '', end_cursor: 'end' })
    await screen.findByText(/log line 19/)
    await waitFor(() => {
      expect(scrollTopValue).toBe(5000)
    })
  })
})
