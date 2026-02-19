/**
 * Formats a JSON audit detail string into a human-readable key=value display.
 * @param {string} detail - JSON string of audit parameters
 * @returns {string} Formatted detail or '-' if empty
 */
export function formatDetail(detail) {
  if (!detail) return '-'
  try {
    const obj = JSON.parse(detail)
    return Object.entries(obj)
      .map(([k, v]) => {
        if (typeof v === 'object' && v !== null) {
          return `${k}=${JSON.stringify(v)}`
        }
        return `${k}=${v}`
      })
      .join(', ')
  } catch {
    return detail
  }
}
