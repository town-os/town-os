/**
 * @typedef {Object} Filesystem
 * @property {string} name
 * @property {number} quota
 * @property {string} [state]
 */

/**
 * @typedef {Object} UnitStatus
 * @property {string} Name
 * @property {string} Description
 * @property {string} LoadState
 * @property {string} ActiveState
 * @property {string} SubState
 */

/**
 * @typedef {Object} JournalEntry
 * @property {string} Cursor
 * @property {string} RealtimeTimestamp
 * @property {number} MonotonicTimestamp
 * @property {string} Message
 * @property {string} MessageID
 * @property {string} Priority
 * @property {string} CodeFile
 * @property {string} CodeLine
 * @property {string} CodeFunc
 * @property {string} Errno
 * @property {string} SyslogFacility
 * @property {string} SyslogIdentifier
 * @property {string} SyslogPID
 * @property {string} PID
 * @property {string} UID
 * @property {string} GID
 * @property {string} Comm
 * @property {string} Exe
 * @property {string} Cmdline
 * @property {string} CapEffective
 * @property {string} AuditSession
 * @property {string} AuditLoginUID
 * @property {string} SystemdCGroup
 * @property {string} SystemdSession
 * @property {string} SystemdUnit
 * @property {string} SystemdUserUnit
 * @property {string} SystemdOwnerUID
 * @property {string} SystemdSlice
 * @property {string} SELinuxContext
 * @property {string} SourceRealtimeTimestamp
 * @property {string} BootID
 * @property {string} MachineID
 * @property {string} Hostname
 * @property {string} Transport
 */

/** @typedef {"start" | "stop" | "restart"} StatusAction */

/**
 * @typedef {Object} Account
 * @property {string} username
 * @property {string} email
 * @property {string} phone
 * @property {string} real_name
 * @property {boolean} admin
 * @property {boolean} disabled
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
 */

/**
 * @typedef {Object} Session
 * @property {string} id
 * @property {string} username
 * @property {string} created_at
 * @property {string} last_used
 */

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
 * @typedef {Object} UnitCounts
 * @property {number} total
 * @property {number} active
 * @property {number} failed
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
 */

/**
 * @typedef {Object} AuthenticateResponse
 * @property {string} token
 * @property {Account} account
 */

export {}
