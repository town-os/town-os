import derive from './derive.js'
import enGB from './en-GB.js'

/**
 * Indian English follows British spelling, supplied by inheriting en-GB. Where
 * Indian English is genuinely distinctive — lakh and crore for large numbers,
 * its own register of official phrasing — this catalog spells no number as a
 * word and has no official register to depart from.
 *
 * Empty is the finding, not a gap.
 */
export const enINOverrides = {}

/** English (India) — en-GB unchanged, reviewed for Indian usage. */
const enIN = derive(enGB, enINOverrides)

export default enIN
