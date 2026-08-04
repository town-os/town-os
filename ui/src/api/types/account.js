/**
 * @typedef {Object} Account
 * @property {string} username
 * @property {string} email
 * @property {string} phone
 * @property {string} real_name
 * @property {boolean} admin
 * @property {boolean} disabled
 * @property {string[]} grants - Named capabilities this account holds. Empty is an ordinary dashboard account; an administrator holds every grant implicitly.
 * @property {string[]} networks - Networks the account acts on. Meaningful only when `grants` is non-empty, and never empty then.
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
 * @property {string[]} [grants] - Replace the grant set. A non-empty set requires a non-empty `networks`, and is rejected on an administrator.
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
