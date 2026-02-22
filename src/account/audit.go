package account

import "time"

type AuditEntry struct {
	ID        int64     `json:"id"`
	Account   string    `json:"account"`
	Action    string    `json:"action"`
	Path      string    `json:"path"`
	Detail    string    `json:"detail"`
	Success   bool      `json:"success"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditListOptions struct {
	BeforeID  int64  `json:"before_id"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
	Account   string `json:"account"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
	Search    string `json:"search"`
}

type AuditPage struct {
	Entries    []AuditEntry `json:"entries"`
	HasMore    bool         `json:"has_more"`
	TotalPages int          `json:"total_pages"`
	TotalCount int          `json:"total_count"`
}

type AuditManager interface {
	LogEntry(entry AuditEntry) error
	List(opts AuditListOptions) (*AuditPage, error)
	CountRecentErrors(since time.Time) (int, error)
}

var RouteActions = map[string]string{
	"/storage/create":         "create filesystem",
	"/storage/modify":         "modify filesystem",
	"/storage/remove":         "remove filesystem",
	"/repository/add":         "add repository",
	"/repository/remove":      "remove repository",
	"/repository/move":        "move repository",
	"/repository/refresh":     "refresh repositories",
	"/packages/install":                   "install package",
	"/packages/uninstall":                 "uninstall package",
	"/packages/purge-uninstalled-volumes": "purge uninstalled volumes",
	"/packages/disable":       "disable package",
	"/packages/enable":        "enable package",
	"/systemd/status":         "set unit status",
	"/account/create":         "create account",
	"/account/update":         "update account",
	"/account/disable":        "disable account",
	"/account/authenticate":   "authenticate",
	"/account/session/revoke": "revoke session",
	"/settings/set":           "update setting",
}
