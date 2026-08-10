import deDE from './de-DE.js'
import derive from './derive.js'

/**
 * Austrian German differs from the German of Germany in month names (Jänner),
 * in food and administrative vocabulary, and in the gender of a handful of
 * nouns. None of that appears here. Austria keeps ß, unlike Switzerland, so
 * even the one orthographic rule that reaches de-CH.js does not reach this
 * file. The computing vocabulary is the shared one: Datei, Sitzung,
 * Berechtigung, Speicher.
 *
 * Empty is the finding, not a gap.
 */
export const deATOverrides = {}

/** German (Austria) — de-DE unchanged, reviewed for Austrian usage. */
const deAT = derive(deDE, deATOverrides)

export default deAT
