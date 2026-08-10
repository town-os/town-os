import derive from './derive.js'
import enUS from './en-US.js'

/**
 * Canadian English is a split decision: British -our and -re endings, American
 * -ize ones. This catalog contains no -our or -re word at all, so the only rule
 * that reaches it is -ize — and on that rule Canada agrees with the United
 * States. *initialize* is correct Canadian spelling.
 *
 * That is why en-CA inherits en-US while en-AU, en-IN, en-NZ and en-ZA inherit
 * en-GB. Empty is the finding, not a gap.
 */
export const enCAOverrides = {}

/** English (Canada) — en-US unchanged, reviewed for Canadian usage. */
const enCA = derive(enUS, enCAOverrides)

export default enCA
