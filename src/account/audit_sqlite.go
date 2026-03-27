package account

import (
	"context"
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
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS audit_log (
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
	_, err = db.ExecContext(ctx, `ALTER TABLE audit_log ADD COLUMN detail TEXT NOT NULL DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return nil, fmt.Errorf("add detail column: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(id DESC)`)
	if err != nil {
		return nil, fmt.Errorf("create idx_audit_created: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_audit_account ON audit_log(account, id DESC)`)
	if err != nil {
		return nil, fmt.Errorf("create idx_audit_account: %w", err)
	}

	if _, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_log(created_at)`); err != nil {
		return nil, fmt.Errorf("create idx_audit_created_at: %w", err)
	}

	if _, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_audit_success_created ON audit_log(success, created_at)`); err != nil {
		return nil, fmt.Errorf("create idx_audit_success_created: %w", err)
	}

	return &SQLiteAuditManager{db: db}, nil
}

func (m *SQLiteAuditManager) LogEntry(entry AuditEntry) error {
	_, err := m.db.ExecContext(context.Background(),
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
	var qb strings.Builder
	qb.WriteString("SELECT id, account, action, path, detail, success, error, created_at, COUNT(*) OVER() FROM audit_log")

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
		qb.WriteString(" WHERE " + strings.Join(where, " AND "))
	}

	sortCol := "id"
	if opts.SortBy != "" && auditAllowedSortColumns[opts.SortBy] {
		sortCol = opts.SortBy
	}
	sortDir := "DESC"
	if strings.EqualFold(opts.SortOrder, "asc") {
		sortDir = "ASC"
	}

	fmt.Fprintf(&qb, " ORDER BY %s %s LIMIT ?", sortCol, sortDir)
	args = append(args, limit+1)

	if opts.Offset > 0 {
		qb.WriteString(" OFFSET ?")
		args = append(args, opts.Offset)
	}

	query := qb.String()
	rows, err := m.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	entries := make([]AuditEntry, 0, limit)
	var total int
	for rows.Next() {
		var e AuditEntry
		var createdStr string

		if err := rows.Scan(&e.ID, &e.Account, &e.Action, &e.Path, &e.Detail, &e.Success, &e.Error, &createdStr, &total); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}

		e.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}

		entries = append(entries, e)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}

	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	return &AuditPage{Entries: entries, HasMore: hasMore, TotalPages: totalPages, TotalCount: total}, nil
}

func (m *SQLiteAuditManager) CountRecentErrors(since time.Time) (int, error) {
	var count int
	err := m.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE success = 0 AND created_at >= ?`,
		since.UTC().Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recent errors: %w", err)
	}
	return count, nil
}
