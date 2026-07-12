import { clsx } from "clsx";
import { twMerge } from "tailwind-merge"

export function cn(...inputs) {
  return twMerge(clsx(inputs));
}

/**
 * Default number of items per page used by all paginated views.
 */
export const PAGE_SIZE = 20

/**
 * Copy text to the clipboard. Falls back to a hidden textarea and
 * document.execCommand when the page is not a secure context (plain HTTP on a
 * non-localhost hostname, which is how most boxes are reached on the LAN -- so
 * the fallback is the path nearly every real install takes, not a rare one).
 *
 * `container` is where the scratch textarea is mounted, and it matters: a
 * textarea on document.body sits OUTSIDE a modal's focus scope, and the dialog
 * yanks focus straight back out of it. The textarea loses its selection, and
 * execCommand('copy') then copies whatever else was selected on the page --
 * usually nothing -- while still reporting success. Callers inside a dialog must
 * pass an element within it so focus and selection survive the copy.
 *
 * @param {string} text - the text to place on the clipboard
 * @param {Element} [container] - element to mount the scratch textarea in;
 *   defaults to document.body, which is only safe outside a focus-trapped dialog
 * @returns {Promise<void>} resolves on success, rejects when the copy failed
 */
export async function copyToClipboard(text, container) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
    return
  }

  const host = container ?? document.body
  const previous = document.activeElement

  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  // Positioned rather than off-screen-fixed: an element inside the dialog needs
  // to stay in the layout to be focusable, but must not be seen or interacted
  // with. Zero opacity with no pointer events keeps it invisible and inert.
  ta.style.position = 'absolute'
  ta.style.opacity = '0'
  ta.style.pointerEvents = 'none'
  ta.style.height = '1px'
  ta.style.width = '1px'
  host.appendChild(ta)

  try {
    ta.focus()
    ta.select()
    // Safari/iOS ignore select() on a readonly textarea; the explicit range is
    // what actually marks the text as the document selection to be copied.
    ta.setSelectionRange(0, text.length)
    if (!document.execCommand('copy')) {
      throw new Error('clipboard copy failed')
    }
  } finally {
    ta.remove()
    if (previous instanceof HTMLElement) {
      previous.focus()
    }
  }
}

/**
 * Format a byte count into a human-readable string (e.g. "1.5 GB").
 * Returns "0 B" for zero values.
 * @param {number} bytes - the byte count to format
 * @returns {string} formatted string with unit
 */
export function formatBytes(bytes) {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const val = bytes / Math.pow(1024, i)
  return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
}
