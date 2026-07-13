/**
 * @typedef {Object} Account
 * @property {string} username
 * @property {string} email
 * @property {string} phone
 * @property {string} real_name
 * @property {boolean} admin
 * @property {boolean} disabled
 * @property {boolean} wireguard - When true, a WireGuard-only account restricted to enrolling on the networks in `networks`.
 * @property {string[]} networks - Networks a WireGuard-only account may enroll peers on. Meaningful only when `wireguard` is true.
 * @property {string} created_at
 * @property {string} updated_at
 */

/**
 * @typedef {Object} UpdateFields
 * @property {string} [password]
 * @property {string} [email]
 * @property {string} [phone]
 * @property {string} [real_name]
 * @property {boolean} [admin]
 * @property {boolean} [wireguard] - Toggle the WireGuard-only restriction. Enabling it requires a non-empty `networks`.
 * @property {string[]} [networks] - Replace the WireGuard-only network scope.
 */

/**
 * @typedef {Object} Session
 * @property {string} id
 * @property {string} username
 * @property {string} created_at
 * @property {string} last_used
 */

export {}
