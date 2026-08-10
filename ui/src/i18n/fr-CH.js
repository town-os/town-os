import derive from './derive.js'
import frFR from './fr-FR.js'

/**
 * Swiss French departs from the French of France in its numerals — septante,
 * huitante, nonante — and in cantonal administrative vocabulary. Neither
 * appears in a control panel for storage volumes and systemd units.
 *
 * Empty is the finding, not a gap.
 */
export const frCHOverrides = {}

/** French (Switzerland) — fr-FR unchanged, reviewed for Swiss usage. */
const frCH = derive(frFR, frCHOverrides)

export default frCH
