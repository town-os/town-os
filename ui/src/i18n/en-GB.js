import derive from './derive.js'
import enUS from './en-US.js'

/**
 * British English departs from American English in a handful of well-known
 * rules, and this catalog is a poor target for most of them: it contains no
 * colour, no licence, no catalogue, no centre, and it already spells
 * *cancelled* the British way. What is left is -ise/-ize, and that reaches
 * exactly two strings.
 *
 * A short list here is a fact about the text, not a shortcut. Technical prose
 * about DNS zones and btrfs subvolumes is written in vocabulary the two
 * Englishes share.
 */
export const enGBOverrides = {
  'dns.disabled_message':
    'DNS service is not enabled. Run Setup DNS to initialise the DNS zone and register packages.',
  'dns.setup_confirm_message':
    'This will initialise the DNS zone and register all installed packages. This operation is idempotent and safe to run multiple times.',
}

/**
 * English (United Kingdom) — en-US with British spelling. Also the base for
 * the Englishes that follow British spelling: en-AU, en-IN, en-NZ, en-ZA.
 */
const enGB = derive(enUS, enGBOverrides)

export default enGB
