/** @import { UnitCounts } from './unit.js' */

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
 */

/**
 * @typedef {Object} AuthenticateResponse
 * @property {string} token
 * @property {import('./account.js').Account} account
 */

export {}
