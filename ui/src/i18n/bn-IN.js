import bnBD from './bn-BD.js'
import derive from './derive.js'

/**
 * Bengali is one language across the border. West Bengal and Bangladesh share
 * an orthography, and what divergence there is is a matter of register —
 * Bangladesh's formal writing carries more Perso-Arabic vocabulary, India's
 * more Sanskritic — which surfaces in literary and administrative prose rather
 * than in software. The computing terms this catalog needs are the same
 * borrowed English ones on both sides: ফাইলসিস্টেম, সেটিংস, আর্কাইভ.
 *
 * Empty is the finding, not a gap.
 */
export const bnINOverrides = {}

/** Bengali (India) — bn-BD unchanged, reviewed for Indian usage. */
const bnIN = derive(bnBD, bnINOverrides)

export default bnIN
