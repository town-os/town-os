import derive from './derive.js'
import enGB from './en-GB.js'

/**
 * Australian English follows British spelling, which is why en-AU inherits
 * en-GB rather than en-US — that alone gets *initialise* right. Beyond
 * spelling, Australian English is distinctive about everyday nouns, and a
 * console that talks about subvolumes, quotas and systemd units never reaches
 * for one.
 *
 * Empty is the finding, not a gap.
 */
export const enAUOverrides = {}

/** English (Australia) — en-GB unchanged, reviewed for Australian usage. */
const enAU = derive(enGB, enAUOverrides)

export default enAU
