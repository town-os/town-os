package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLitePagesManager struct {
	db *sql.DB
}

func InitPagesManager(db *sql.DB) (*SQLitePagesManager, error) {
	_, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS pages (
		name       TEXT PRIMARY KEY,
		repo_url   TEXT NOT NULL,
		branch     TEXT NOT NULL DEFAULT 'main',
		domain     TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'pending',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create pages table: %w", err)
	}

	return &SQLitePagesManager{db: db}, nil
}

func (m *SQLitePagesManager) Create(name, repoURL, branch, domain string) (*PageSite, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrPageNameRequired
	}
	if strings.TrimSpace(repoURL) == "" {
		return nil, ErrPageRepoRequired
	}
	if strings.TrimSpace(domain) == "" {
		return nil, ErrPageDomainRequired
	}
	if branch == "" {
		branch = "main"
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	_, err := m.db.ExecContext(context.Background(),
		`INSERT INTO pages (name, repo_url, branch, domain, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
		name, repoURL, branch, domain, nowStr, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrDuplicatePageName
		}
		return nil, fmt.Errorf("insert page: %w", err)
	}

	return &PageSite{
		Name:      name,
		RepoURL:   repoURL,
		Branch:    branch,
		Domain:    domain,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *SQLitePagesManager) Get(name string) (*PageSite, error) {
	var page PageSite
	var createdStr, updatedStr string

	err := m.db.QueryRowContext(context.Background(),
		`SELECT name, repo_url, branch, domain, status, created_at, updated_at FROM pages WHERE name = ?`,
		name,
	).Scan(&page.Name, &page.RepoURL, &page.Branch, &page.Domain, &page.Status, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get page: %w", err)
	}

	var parseErr error
	page.CreatedAt, parseErr = time.Parse(time.RFC3339, createdStr)
	if parseErr != nil {
		return nil, fmt.Errorf("parse created_at: %w", parseErr)
	}
	page.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedStr)
	if parseErr != nil {
		return nil, fmt.Errorf("parse updated_at: %w", parseErr)
	}

	return &page, nil
}

func (m *SQLitePagesManager) Update(name string, fields PageSiteUpdate) (*PageSite, error) {
	var sets []string
	var args []any

	if fields.RepoURL != nil {
		if strings.TrimSpace(*fields.RepoURL) == "" {
			return nil, ErrPageRepoRequired
		}
		sets = append(sets, "repo_url = ?")
		args = append(args, *fields.RepoURL)
	}
	if fields.Branch != nil {
		sets = append(sets, "branch = ?")
		args = append(args, *fields.Branch)
	}
	if fields.Domain != nil {
		if strings.TrimSpace(*fields.Domain) == "" {
			return nil, ErrPageDomainRequired
		}
		sets = append(sets, "domain = ?")
		args = append(args, *fields.Domain)
	}
	if fields.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *fields.Status)
	}

	if len(sets) == 0 {
		return m.Get(name)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, nowStr)
	args = append(args, name)

	res, err := m.db.ExecContext(context.Background(),
		fmt.Sprintf("UPDATE pages SET %s WHERE name = ?", strings.Join(sets, ", ")),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("update page: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, ErrPageNotFound
	}

	return m.Get(name)
}

func (m *SQLitePagesManager) Remove(name string) error {
	res, err := m.db.ExecContext(context.Background(), "DELETE FROM pages WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete page: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrPageNotFound
	}

	return nil
}

func (m *SQLitePagesManager) List() (_ []PageSite, err error) {
	rows, err := m.db.QueryContext(context.Background(),
		`SELECT name, repo_url, branch, domain, status, created_at, updated_at FROM pages ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var out []PageSite
	for rows.Next() {
		var page PageSite
		var createdStr, updatedStr string

		if err := rows.Scan(&page.Name, &page.RepoURL, &page.Branch, &page.Domain, &page.Status, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan page row: %w", err)
		}

		page.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		page.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}

		out = append(out, page)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return out, nil
}
