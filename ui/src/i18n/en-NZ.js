import derive from './derive.js'
import enGB from './en-GB.js'

/**
 * New Zealand English follows British spelling, supplied by inheriting en-GB.
 * Its distinctive vocabulary lies outside anything a storage and service
 * console says.
 *
 * Empty is the finding, not a gap.
 */
export const enNZOverrides = {}

/** English (New Zealand) — en-GB unchanged, reviewed for New Zealand usage. */
const enNZ = derive(enGB, enNZOverrides)

export default enNZ
