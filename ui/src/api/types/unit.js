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
 * @typedef {Object} UnitCounts
 * @property {number} total
 * @property {number} active
 * @property {number} failed
 */

export {}
