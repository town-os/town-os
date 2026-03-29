package account

import (
	"database/sql"
	"errors"
	"fmt"
)

type SQLiteSettingsManager struct {
	db *sql.DB
}

func InitSettingsManager(db *sql.DB) (*SQLiteSettingsManager, error) {
	ctx, cancel := dbCtx()
	defer cancel()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return nil, fmt.Errorf("create settings table: %w", err)
	}

	// Seed default values; INSERT OR IGNORE preserves existing rows.
	for k, v := range DefaultSettings {
		_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`, k, v)
		if err != nil {
			return nil, fmt.Errorf("seed default setting %q: %w", k, err)
		}
	}

	return &SQLiteSettingsManager{db: db}, nil
}

func (m *SQLiteSettingsManager) Get(key string) (string, error) {
	ctx, cancel := dbCtx()
	defer cancel()

	var value string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("setting %q not found", key)
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

func (m *SQLiteSettingsManager) Set(key, value string) error {
	ctx, cancel := dbCtx()
	defer cancel()

	_, err := m.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

func (m *SQLiteSettingsManager) List() (_ map[string]string, err error) {
	ctx, cancel := dbCtx()
	defer cancel()

	rows, err := m.db.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		err := rows.Scan(&key, &value)
		if err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out[key] = value
	}

	return out, rows.Err()
}
