/**
 * Barrel re-export of all domain-specific types.
 *
 * @typedef {import('./types/storage.js').Filesystem} Filesystem
 *
 * @typedef {import('./types/unit.js').UnitStatus} UnitStatus
 * @typedef {import('./types/unit.js').JournalEntry} JournalEntry
 * @typedef {import('./types/unit.js').StatusAction} StatusAction
 * @typedef {import('./types/unit.js').UnitCounts} UnitCounts
 *
 * @typedef {import('./types/account.js').Account} Account
 * @typedef {import('./types/account.js').UpdateFields} UpdateFields
 * @typedef {import('./types/account.js').Session} Session
 *
 * @typedef {import('./types/audit.js').AuditEntry} AuditEntry
 * @typedef {import('./types/audit.js').AuditListOptions} AuditListOptions
 * @typedef {import('./types/audit.js').AuditPage} AuditPage
 *
 * @typedef {import('./types/package.js').Question} Question
 * @typedef {import('./types/package.js').Responses} Responses
 * @typedef {import('./types/package.js').InstalledInfo} InstalledInfo
 * @typedef {import('./types/package.js').RepositoryInfo} RepositoryInfo
 * @typedef {import('./types/package.js').PackageUpgrade} PackageUpgrade
 *
 * @typedef {import('./types/ping.js').PingResponse} PingResponse
 * @typedef {import('./types/ping.js').AuthenticateResponse} AuthenticateResponse
 */

/**
 * @typedef {Object} PageSite
 * @property {string} name
 * @property {string} repo_url
 * @property {string} branch
 * @property {string} domain
 * @property {string} status
 * @property {string} created_at
 * @property {string} updated_at
 */

/**
 * @typedef {Object} PageSiteUpdate
 * @property {string} [repo_url]
 * @property {string} [branch]
 * @property {string} [domain]
 * @property {string} [status]
 */

export {}
