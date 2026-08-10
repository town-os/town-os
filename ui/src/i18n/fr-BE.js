import derive from './derive.js'
import frFR from './fr-FR.js'

/**
 * Belgian French departs from the French of France in its numerals — septante
 * and nonante — and in a set of institutional terms. This catalog spells no
 * number as a word and names no institution. Unlike Canadian French, Belgian
 * French does not systematically avoid English computing loans, so *repository*
 * and *e-mail* stand as fr-FR writes them.
 *
 * Empty is the finding, not a gap.
 */
export const frBEOverrides = {}

/** French (Belgium) — fr-FR unchanged, reviewed for Belgian usage. */
const frBE = derive(frFR, frBEOverrides)

export default frBE
