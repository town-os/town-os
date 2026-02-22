package account

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var auditAllowedSortColumns = map[string]bool{
	"id":         true,
	"account":    true,
	"action":     true,
	"path":       true,
	"detail":     true,
	"success":    true,
	"error":      true,
	"created_at": true,
}

const (
	auditDefaultLimit = 50
	auditMaxLimit     = 200
)

type SQLiteAuditManager struct {
	db *sql.DB
}

func InitAuditManager(db *sql.DB) (*SQLiteAuditManager, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS audit_log (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		account    TEXT NOT NULL DEFAULT '',
		action     TEXT NOT NULL,
		path       TEXT NOT NULL,
		detail     TEXT NOT NULL DEFAULT '',
		success    INTEGER NOT NULL DEFAULT 1,
		error      TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create audit_log table: %w", err)
	}

	// Migrate: add detail column if it does not exist (for existing databases).
	_, _ = db.Exec(`ALTER TABLE audit_log ADD COLUMN detail TEXT NOT NULL DEFAULT ''`)

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(id DESC)`); err != nil {
		return nil, fmt.Errorf("create idx_audit_created: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_account ON audit_log(account, id DESC)`); err != nil {
		return nil, fmt.Errorf("create idx_audit_account: %w", err)
	}

	return &SQLiteAuditManager{db: db}, nil
}

func (m *SQLiteAuditManager) LogEntry(entry AuditEntry) error {
	_, err := m.db.Exec(
		`INSERT INTO audit_log (account, action, path, detail, success, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Account, entry.Action, entry.Path, entry.Detail, entry.Success, entry.Error,
		entry.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

func (m *SQLiteAuditManager) List(opts AuditListOptions) (_ *AuditPage, err error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = auditDefaultLimit
	}
	if limit > auditMaxLimit {
		limit = auditMaxLimit
	}

	var args []any
	query := "SELECT id, account, action, path, detail, success, error, created_at FROM audit_log"

	var where []string
	if opts.BeforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, opts.BeforeID)
	}
	if opts.Account != "" {
		where = append(where, "account = ?")
		args = append(args, opts.Account)
	}
	if opts.Search != "" {
		pattern := fmt.Sprintf("%%%s%%", opts.Search)
		where = append(where, "(account LIKE ? OR action LIKE ? OR path LIKE ? OR detail LIKE ?)")
		args = append(args, pattern, pattern, pattern, pattern)
	}

	if len(where) > 0 {
		query += " WHERE " + where[0]
		for _, w := range where[1:] {
			query += " AND " + w
		}
	}

	sortCol := "id"
	if opts.SortBy != "" && auditAllowedSortColumns[opts.SortBy] {
		sortCol = opts.SortBy
	}
	sortDir := "DESC"
	if strings.EqualFold(opts.SortOrder, "asc") {
		sortDir = "ASC"
	}

	query += fmt.Sprintf(" ORDER BY %s %s LIMIT ?", sortCol, sortDir)
	args = append(args, limit+1)

	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var createdStr string

		if err := rows.Scan(&e.ID, &e.Account, &e.Action, &e.Path, &e.Detail, &e.Success, &e.Error, &createdStr); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}

		e.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}

		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}

	countQuery := "SELECT COUNT(*) FROM audit_log"
	var countWhere []string
	var countArgs []any
	if opts.Account != "" {
		countWhere = append(countWhere, "account = ?")
		countArgs = append(countArgs, opts.Account)
	}
	if opts.Search != "" {
		pattern := fmt.Sprintf("%%%s%%", opts.Search)
		countWhere = append(countWhere, "(account LIKE ? OR action LIKE ? OR path LIKE ? OR detail LIKE ?)")
		countArgs = append(countArgs, pattern, pattern, pattern, pattern)
	}
	if len(countWhere) > 0 {
		countQuery += fmt.Sprintf(" WHERE %s", strings.Join(countWhere, " AND "))
	}

	var total int
	if err := m.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count audit entries: %w", err)
	}

	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	return &AuditPage{Entries: entries, HasMore: hasMore, TotalPages: totalPages, TotalCount: total}, nil
}

func (m *SQLiteAuditManager) CountRecentErrors(since time.Time) (int, error) {
	var count int
	err := m.db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE success = 0 AND created_at >= ?`,
		since.UTC().Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recent errors: %w", err)
	}
	return count, nil
}
