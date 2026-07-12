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
 * non-localhost hostname, which is how most boxes are reached on the LAN).
 * @param {string} text - the text to place on the clipboard
 * @returns {Promise<void>} resolves on success, rejects when the copy failed
 */
export async function copyToClipboard(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
    return
  }

  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  try {
    if (!document.execCommand('copy')) {
      throw new Error('clipboard copy failed')
    }
  } finally {
    document.body.removeChild(ta)
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
