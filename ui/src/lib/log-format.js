// Greyscale ANSI mapping: darker codes get darker greys, brighter get lighter.
export const ANSI_GREYS = {
  '30': '#3a3a3a', '31': '#888', '32': '#aaa', '33': '#999',
  '34': '#888', '35': '#999', '36': '#aaa', '37': '#ccc',
  '90': '#666', '91': '#888', '92': '#aaa', '93': '#999',
  '94': '#888', '95': '#999', '96': '#aaa', '97': '#ddd',
}

/**
 * Parse ANSI escape codes into segments with color/bold metadata.
 * @param {string} text
 * @returns {{ str: string, color: string|null, bold: boolean }[]}
 */
export function parseAnsi(text) {
  const segments = []
  const re = /\x1b\[([0-9;]*)m/g
  let last = 0
  let color = null
  let bold = false
  let match

  while ((match = re.exec(text)) !== null) {
    if (match.index > last) {
      segments.push({ str: text.slice(last, match.index), color, bold })
    }
    last = match.index + match[0].length

    const codes = match[1].split(';').filter(Boolean)
    for (const code of codes) {
      if (code === '0') {
        color = null
        bold = false
      } else if (code === '1') {
        bold = true
      } else if (ANSI_GREYS[code]) {
        color = ANSI_GREYS[code]
      }
    }
  }

  if (last < text.length) {
    segments.push({ str: text.slice(last), color, bold })
  }

  return segments
}

/**
 * Detect name=value fields in text. Returns array of segments:
 * { type: 'text', value } or { type: 'field', name, eq, value }.
 * @param {string} text
 * @returns {({ type: 'text', value: string } | { type: 'field', name: string, eq: string, value: string })[]}
 */
export function parseFields(text) {
  const parts = []
  const re = /([A-Za-z_][A-Za-z0-9_.]*)(=)(\S*)/g
  let last = 0
  let match

  while ((match = re.exec(text)) !== null) {
    if (match.index > last) {
      parts.push({ type: 'text', value: text.slice(last, match.index) })
    }
    parts.push({ type: 'field', name: match[1], eq: match[2], value: match[3] })
    last = match.index + match[0].length
  }

  if (last < text.length) {
    parts.push({ type: 'text', value: text.slice(last) })
  }

  return parts
}

/**
 * Strip ANSI escape codes from text, returning plain text.
 * @param {string} text
 * @returns {string}
 */
export function stripAnsi(text) {
  return text.replace(/\x1b\[[0-9;]*m/g, '')
}

/**
 * Group journal entries by one-minute windows.
 * @param {{ RealtimeTimestamp: string }[]} entries
 * @returns {{ key: string, label: string, entries: object[] }[]}
 */
export function groupByMinute(entries) {
  const groups = []
  let current = null
  for (const entry of entries) {
    const d = new Date(entry.RealtimeTimestamp)
    const key = `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}-${d.getHours()}-${d.getMinutes()}`
    if (!current || current.key !== key) {
      current = { key, label: d.toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' }), entries: [] }
      groups.push(current)
    }
    current.entries.push(entry)
  }
  return groups
}
