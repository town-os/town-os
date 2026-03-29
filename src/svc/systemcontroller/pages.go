package systemcontroller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// pagesDBTimeout is the default timeout for pages database operations.
const pagesDBTimeout = 30 * time.Second

var pagesAllowedSortColumns = map[string]bool{
	"name":        true,
	"repo_url":    true,
	"branch":      true,
	"domain":      true,
	"source_type": true,
	"status":      true,
	"created_at":  true,
	"updated_at":  true,
}

const (
	pagesDefaultLimit = 50
	pagesMaxLimit     = 200

	PageStatusPending = "pending"
	PageStatusActive  = "active"
	PageStatusError   = "error"
)

type Page struct {
	Name           string `json:"name"`
	RepoURL        string `json:"repo_url"`
	Branch         string `json:"branch"`
	Domain         string `json:"domain"`
	SourceType     string `json:"source_type"`
	Image          string `json:"image"`
	ImageDirectory string `json:"image_directory"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type PagesListOptions struct {
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	Search    string `json:"search"`
}

type PagesPage struct {
	Entries    []Page `json:"entries"`
	HasMore    bool   `json:"has_more"`
	TotalPages int    `json:"total_pages"`
	TotalCount int    `json:"total_count"`
}

type PagesStore struct {
	db *sql.DB
}

func InitPagesStore(db *sql.DB) (*PagesStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer cancel()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS pages (
		name            TEXT PRIMARY KEY,
		repo_url        TEXT NOT NULL DEFAULT '',
		branch          TEXT NOT NULL DEFAULT 'main',
		domain          TEXT NOT NULL,
		source_type     TEXT NOT NULL DEFAULT 'git',
		image           TEXT NOT NULL DEFAULT '',
		image_directory TEXT NOT NULL DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'pending',
		created_at      TEXT NOT NULL,
		updated_at      TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create pages table: %w", err)
	}

	// Migrate existing tables that lack the new columns.
	for _, col := range []struct {
		name string
		def  string
	}{
		{"source_type", "TEXT NOT NULL DEFAULT 'git'"},
		{"image", "TEXT NOT NULL DEFAULT ''"},
		{"image_directory", "TEXT NOT NULL DEFAULT ''"},
	} {
		_, err := db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE pages ADD COLUMN %s %s", col.name, col.def))
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate pages column %s: %w", col.name, err)
		}
	}

	return &PagesStore{db: db}, nil
}

func (s *PagesStore) Create(page Page) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if page.Branch == "" {
		page.Branch = "main"
	}
	if page.Domain == "" {
		page.Domain = page.Name
	}
	if page.Status == "" {
		page.Status = PageStatusPending
	}
	if page.SourceType == "" {
		page.SourceType = "archive"
	}

	ctx, cancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pages (name, repo_url, branch, domain, source_type, image, image_directory, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		page.Name, page.RepoURL, page.Branch, page.Domain, page.SourceType, page.Image, page.ImageDirectory, page.Status, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert page: %w", err)
	}
	return nil
}

func (s *PagesStore) Update(name string, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}

	allowed := map[string]bool{"repo_url": true, "branch": true, "domain": true, "source_type": true, "image": true, "image_directory": true, "status": true}

	var sets []string
	var args []any
	for k, v := range updates {
		if !allowed[k] {
			continue
		}
		sets = append(sets, k+" = ?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, name)

	query := "UPDATE pages SET " + strings.Join(sets, ", ") + " WHERE name = ?" //nolint:gosec // G202 -- columns from allowlist, not user input

	ctx, cancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer cancel()

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update page: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("page %q not found", name)
	}

	return nil
}

func (s *PagesStore) Remove(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer cancel()

	result, err := s.db.ExecContext(ctx, `DELETE FROM pages WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete page: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("page %q not found", name)
	}

	return nil
}

func (s *PagesStore) Get(name string) (*Page, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer cancel()

	row := s.db.QueryRowContext(ctx,
		`SELECT name, repo_url, branch, domain, source_type, image, image_directory, status, created_at, updated_at FROM pages WHERE name = ?`, name)

	var p Page
	err := row.Scan(&p.Name, &p.RepoURL, &p.Branch, &p.Domain, &p.SourceType, &p.Image, &p.ImageDirectory, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("page %q not found", name)
		}
		return nil, fmt.Errorf("scan page: %w", err)
	}

	return &p, nil
}

const pagesSelectColumns = "name, repo_url, branch, domain, source_type, image, image_directory, status, created_at, updated_at"

func (s *PagesStore) List(opts PagesListOptions) (_ *PagesPage, err error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = pagesDefaultLimit
	}
	if limit > pagesMaxLimit {
		limit = pagesMaxLimit
	}

	var args []any
	var qb strings.Builder
	qb.WriteString("SELECT " + pagesSelectColumns + " FROM pages")

	var where []string
	if opts.Search != "" {
		pattern := fmt.Sprintf("%%%s%%", opts.Search)
		where = append(where, "(name LIKE ? OR repo_url LIKE ? OR domain LIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}

	if len(where) > 0 {
		fmt.Fprintf(&qb, " WHERE %s", strings.Join(where, " AND "))
	}

	sortCol := "name"
	if opts.SortBy != "" && pagesAllowedSortColumns[opts.SortBy] {
		sortCol = opts.SortBy
	}
	sortDir := "ASC"
	if strings.EqualFold(opts.SortOrder, "desc") {
		sortDir = "DESC"
	}

	fmt.Fprintf(&qb, " ORDER BY %s %s LIMIT ?", sortCol, sortDir)
	args = append(args, limit+1)

	if opts.Offset > 0 {
		qb.WriteString(" OFFSET ?")
		args = append(args, opts.Offset)
	}

	query := qb.String()

	ctx, cancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pages: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var entries []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.Name, &p.RepoURL, &p.Branch, &p.Domain, &p.SourceType, &p.Image, &p.ImageDirectory, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		entries = append(entries, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}

	// Count total.
	countQuery := "SELECT COUNT(*) FROM pages"
	var countArgs []any
	if opts.Search != "" {
		pattern := fmt.Sprintf("%%%s%%", opts.Search)
		countQuery += " WHERE (name LIKE ? OR repo_url LIKE ? OR domain LIKE ?)"
		countArgs = append(countArgs, pattern, pattern, pattern)
	}

	var total int
	countCtx, countCancel := context.WithTimeout(context.Background(), pagesDBTimeout)
	defer countCancel()

	if err := s.db.QueryRowContext(countCtx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count pages: %w", err)
	}

	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	return &PagesPage{Entries: entries, HasMore: hasMore, TotalPages: totalPages, TotalCount: total}, nil
}
