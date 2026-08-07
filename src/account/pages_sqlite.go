package account

import (
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
	ctx, cancel := dbCtx()
	defer cancel()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS pages (
		name            TEXT PRIMARY KEY,
		repo_url        TEXT NOT NULL DEFAULT '',
		branch          TEXT NOT NULL DEFAULT 'main',
		domain          TEXT NOT NULL,
		source_type     TEXT NOT NULL DEFAULT 'archive',
		image           TEXT NOT NULL DEFAULT '',
		image_directory TEXT NOT NULL DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'pending',
		network         TEXT NOT NULL DEFAULT '',
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
		{"source_type", "TEXT NOT NULL DEFAULT 'archive'"},
		{"image", "TEXT NOT NULL DEFAULT ''"},
		{"image_directory", "TEXT NOT NULL DEFAULT ''"},
		// Existing pages predate networks; '' means the default/home network,
		// matching Installer.LoadNetwork's convention for packages.
		{"network", "TEXT NOT NULL DEFAULT ''"},
	} {
		_, err := db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE pages ADD COLUMN %s %s", col.name, col.def))
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate pages column %s: %w", col.name, err)
		}
	}

	return &SQLitePagesManager{db: db}, nil
}

func (m *SQLitePagesManager) Create(name, repoURL, branch, domain, sourceType, image, imageDirectory, network string) (*PageSite, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrPageNameRequired
	}
	// A domain is not a label -- it names this page's subvolume, its webroot
	// symlink, its leaf SAN and its ingress vhost -- so it is checked here,
	// where it enters, rather than by each of those consumers. See
	// ValidatePageDomain.
	if err := ValidatePageDomain(domain); err != nil {
		return nil, err
	}

	if sourceType == "" {
		sourceType = PageSourceArchive
	}
	if !ValidPageSourceType(sourceType) {
		return nil, ErrPageInvalidSourceType
	}

	switch sourceType {
	case PageSourceGit:
		if strings.TrimSpace(repoURL) == "" {
			return nil, ErrPageRepoRequired
		}
	case PageSourceContainerImage:
		if strings.TrimSpace(image) == "" {
			return nil, ErrPageImageRequired
		}
		if strings.TrimSpace(imageDirectory) == "" {
			return nil, ErrPageImageDirectoryRequired
		}
	}

	if branch == "" {
		branch = "main"
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	ctx, cancel := dbCtx()
	defer cancel()

	_, err := m.db.ExecContext(ctx,
		`INSERT INTO pages (name, repo_url, branch, domain, source_type, image, image_directory, status, network, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`,
		name, repoURL, branch, domain, sourceType, image, imageDirectory, network, nowStr, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrDuplicatePageName
		}
		return nil, fmt.Errorf("insert page: %w", err)
	}

	return &PageSite{
		Name:           name,
		RepoURL:        repoURL,
		Branch:         branch,
		Domain:         domain,
		SourceType:     sourceType,
		Image:          image,
		ImageDirectory: imageDirectory,
		Status:         "pending",
		Network:        network,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (m *SQLitePagesManager) Get(name string) (*PageSite, error) {
	var page PageSite
	var createdStr, updatedStr string

	ctx, cancel := dbCtx()
	defer cancel()

	err := m.db.QueryRowContext(ctx,
		`SELECT name, repo_url, branch, domain, source_type, image, image_directory, status, network, created_at, updated_at FROM pages WHERE name = ?`,
		name,
	).Scan(&page.Name, &page.RepoURL, &page.Branch, &page.Domain, &page.SourceType, &page.Image, &page.ImageDirectory, &page.Status, &page.Network, &createdStr, &updatedStr)
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
		sets = append(sets, "repo_url = ?")
		args = append(args, *fields.RepoURL)
	}
	if fields.Branch != nil {
		sets = append(sets, "branch = ?")
		args = append(args, *fields.Branch)
	}
	if fields.Domain != nil {
		// The same check Create applies. Its absence here was the gap: an edit
		// could set a domain that creation would have refused, and migratePageDir
		// then carried it into os.Remove and os.Symlink.
		if err := ValidatePageDomain(*fields.Domain); err != nil {
			return nil, err
		}
		sets = append(sets, "domain = ?")
		args = append(args, *fields.Domain)
	}
	if fields.SourceType != nil {
		if !ValidPageSourceType(*fields.SourceType) {
			return nil, ErrPageInvalidSourceType
		}
		sets = append(sets, "source_type = ?")
		args = append(args, *fields.SourceType)
	}
	if fields.Image != nil {
		sets = append(sets, "image = ?")
		args = append(args, *fields.Image)
	}
	if fields.ImageDirectory != nil {
		sets = append(sets, "image_directory = ?")
		args = append(args, *fields.ImageDirectory)
	}
	if fields.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *fields.Status)
	}
	if fields.Network != nil {
		sets = append(sets, "network = ?")
		args = append(args, *fields.Network)
	}

	if len(sets) == 0 {
		return m.Get(name)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, nowStr)
	args = append(args, name)

	ctx, cancel := dbCtx()
	defer cancel()

	res, err := m.db.ExecContext(ctx,
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
	ctx, cancel := dbCtx()
	defer cancel()

	res, err := m.db.ExecContext(ctx, "DELETE FROM pages WHERE name = ?", name)
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
	ctx, cancel := dbCtx()
	defer cancel()

	rows, err := m.db.QueryContext(ctx,
		`SELECT name, repo_url, branch, domain, source_type, image, image_directory, status, network, created_at, updated_at FROM pages ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	out := make([]PageSite, 0, 64)
	for rows.Next() {
		var page PageSite
		var createdStr, updatedStr string

		if err := rows.Scan(&page.Name, &page.RepoURL, &page.Branch, &page.Domain, &page.SourceType, &page.Image, &page.ImageDirectory, &page.Status, &page.Network, &createdStr, &updatedStr); err != nil {
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
