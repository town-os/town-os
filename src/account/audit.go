package account

import "time"

type AuditEntry struct {
	ID        int64     `json:"id"`
	Account   string    `json:"account"`
	Action    string    `json:"action"`
	Path      string    `json:"path"`
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
}

type AuditPage struct {
	Entries    []AuditEntry `json:"entries"`
	HasMore    bool         `json:"has_more"`
	TotalPages int          `json:"total_pages"`
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
	"/storage":                "list filesystems",
	"/repository/add":         "add repository",
	"/repository/remove":      "remove repository",
	"/repository/refresh":     "refresh repositories",
	"/repository":             "list repositories",
	"/packages":               "list packages",
	"/packages/questions":     "get package questions",
	"/packages/install":       "install package",
	"/packages/uninstall":     "uninstall package",
	"/packages/installed":     "list installed",
	"/packages/responses":     "get responses",
	"/systemd/units":          "list units",
	"/systemd/status":         "set unit status",
	"/systemd/logs":           "replay logs",
	"/systemd/logs/tail":      "tail logs",
	"/account/create":         "create account",
	"/account":                "get account",
	"/account/update":         "update account",
	"/account/disable":        "disable account",
	"/account/authenticate":   "authenticate",
	"/account/session/revoke": "revoke session",
}
