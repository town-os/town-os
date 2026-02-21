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
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return nil, fmt.Errorf("create settings table: %w", err)
	}

	return &SQLiteSettingsManager{db: db}, nil
}

func (m *SQLiteSettingsManager) Get(key string) (string, error) {
	var value string
	err := m.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("setting %q not found", key)
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

func (m *SQLiteSettingsManager) Set(key, value string) error {
	_, err := m.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

func (m *SQLiteSettingsManager) List() (_ map[string]string, err error) {
	rows, err := m.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out[key] = value
	}

	return out, rows.Err()
}
