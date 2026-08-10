import arSA from './ar-SA.js'
import derive from './derive.js'

/**
 * Emirati written Arabic is Gulf-standard Modern Standard Arabic, which is what
 * ar-SA.js already is — including تنزيل for download, the one point where
 * regional written usage genuinely splits (see ar-EG.js for the other side of
 * it). The UAE sits on the same side of that split as Saudi Arabia.
 *
 * Empty is the finding, not a gap.
 */
export const arAEOverrides = {}

/** Arabic (UAE) — ar-SA unchanged, reviewed for Emirati usage. */
const arAE = derive(arSA, arAEOverrides)

export default arAE
