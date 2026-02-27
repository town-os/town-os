/**
 * @typedef {Object} Question
 * @property {string} query
 * @property {string} [type]
 */

/** @typedef {Record<string, string>} Responses */

/**
 * @typedef {Object} InstalledInfo
 * @property {Record<string, Question>} questions
 * @property {Responses} responses
 * @property {Record<string, string>} notes
 */

/**
 * @typedef {Object} RepositoryInfo
 * @property {string} name
 * @property {string} url
 * @property {string} [error]
 */

/**
 * @typedef {Object} PackageUpgrade
 * @property {string} repo
 * @property {string} name
 * @property {string} installed_version
 * @property {string} latest_version
 * @property {boolean} changed
 */

export {}
