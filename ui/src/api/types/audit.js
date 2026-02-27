/**
 * @typedef {Object} AuditEntry
 * @property {number} id
 * @property {string} account
 * @property {string} action
 * @property {string} path
 * @property {string} detail
 * @property {boolean} success
 * @property {string} error
 * @property {string} created_at
 */

/**
 * @typedef {Object} AuditListOptions
 * @property {number} [before_id]
 * @property {number} [limit]
 * @property {string} [account]
 */

/**
 * @typedef {Object} AuditPage
 * @property {AuditEntry[]} entries
 * @property {boolean} has_more
 */

export {}
