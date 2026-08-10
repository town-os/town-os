import derive from './derive.js'
import enGB from './en-GB.js'

/**
 * South African English follows British spelling, supplied by inheriting
 * en-GB. Its distinctive vocabulary is borrowed from Afrikaans and the other
 * official languages and belongs to daily life, not to a message set about
 * btrfs subvolumes and WireGuard peers.
 *
 * Empty is the finding, not a gap.
 */
export const enZAOverrides = {}

/** English (South Africa) — en-GB unchanged, reviewed for South African usage. */
const enZA = derive(enGB, enZAOverrides)

export default enZA
