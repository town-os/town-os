/** @import { UnitCounts } from './unit.js' */

/**
 * Whether this box can host a swapfile on the btrfs pool, and its usage if it
 * does. `supported` is false on every multi-disk install — btrfs will not swap
 * on a multi-device filesystem — and `reason` is a CODE, not a sentence, so the
 * wording lives in the locale catalogs (`storage.swap_reason_<reason>`).
 *
 * @typedef {Object} SwapCapability
 * @property {boolean} supported
 * @property {string} [reason] - multi_device | data_profile | probe_failed
 * @property {number} devices - block devices backing the pool
 * @property {string[]} [data_profiles] - e.g. ["single"], ["RAID5"]
 * @property {string} [path]
 * @property {boolean} active
 * @property {number} [size_bytes]
 * @property {number} [used_bytes]
 */

/**
 * @typedef {Object} PingResponse
 * @property {string} status
 * @property {number} filesystems
 * @property {number} repositories
 * @property {number} packages
 * @property {number} installed
 * @property {number} accounts
 * @property {number} admins
 * @property {UnitCounts} [units]
 * @property {number} recent_errors
 * @property {boolean} needs_setup
 * @property {number} upgrades_available
 * @property {boolean} [upgrades_dismissed]
 * @property {number} timezone_offset - Server UTC offset in minutes (positive = east, negative = west)
 * @property {SwapCapability} [swap]
 */

/**
 * @typedef {Object} AuthenticateResponse
 * @property {string} token
 * @property {import('./account.js').Account} account
 */

export {}
