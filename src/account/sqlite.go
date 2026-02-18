package account

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

type SQLiteManager struct {
	db *sql.DB
}

func OpenDB(path string) (db *sql.DB, err error) {
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		if cerr := db.Close(); cerr != nil {
			return nil, fmt.Errorf("pragma WAL: %w (close: %v)", err, cerr)
		}
		return nil, fmt.Errorf("pragma WAL: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		if cerr := db.Close(); cerr != nil {
			return nil, fmt.Errorf("pragma foreign_keys: %w (close: %v)", err, cerr)
		}
		return nil, fmt.Errorf("pragma foreign_keys: %w", err)
	}

	return db, nil
}

func InitManager(db *sql.DB) (*SQLiteManager, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS accounts (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		email         TEXT NOT NULL,
		phone         TEXT NOT NULL,
		real_name     TEXT NOT NULL,
		admin         INTEGER NOT NULL DEFAULT 0,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create accounts table: %w", err)
	}

	return &SQLiteManager{db: db}, nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	expectedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	key := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expectedKey)))

	if len(key) != len(expectedKey) {
		return false
	}
	// constant-time comparison
	var diff byte
	for i := range key {
		diff |= key[i] ^ expectedKey[i]
	}
	return diff == 0
}

func (m *SQLiteManager) Create(username, password, email, phone, realName string, admin bool) (_ *Account, err error) {
	if err := validateContactInfo(email, phone, realName); err != nil {
		return nil, err
	}

	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	adminInt := 0
	if admin {
		adminInt = 1
	}

	_, err = m.db.Exec(
		`INSERT INTO accounts (username, password_hash, email, phone, real_name, admin, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		username, hash, email, phone, realName, adminInt, nowStr, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrDuplicateUsername
		}
		return nil, fmt.Errorf("insert account: %w", err)
	}

	return &Account{
		Username:  username,
		Email:     email,
		Phone:     phone,
		RealName:  realName,
		Admin:     admin,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *SQLiteManager) Get(username string) (*Account, error) {
	row := m.db.QueryRow(
		`SELECT username, password_hash, email, phone, real_name, admin, created_at, updated_at
		 FROM accounts WHERE username = ?`, username,
	)
	return scanAccount(row)
}

func scanAccount(row *sql.Row) (*Account, error) {
	var acct Account
	var adminInt int
	var createdStr, updatedStr string

	err := row.Scan(&acct.Username, &acct.PasswordHash, &acct.Email, &acct.Phone, &acct.RealName, &adminInt, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan account: %w", err)
	}

	acct.Admin = adminInt != 0

	var parseErr error
	acct.CreatedAt, parseErr = time.Parse(time.RFC3339, createdStr)
	if parseErr != nil {
		return nil, fmt.Errorf("parse created_at: %w", parseErr)
	}
	acct.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedStr)
	if parseErr != nil {
		return nil, fmt.Errorf("parse updated_at: %w", parseErr)
	}

	return &acct, nil
}

func (m *SQLiteManager) Update(username string, fields UpdateFields) (_ *Account, err error) {
	var sets []string
	var args []any

	if fields.Password != nil {
		hash, err := hashPassword(*fields.Password)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "password_hash = ?")
		args = append(args, hash)
	}
	if fields.Email != nil {
		sets = append(sets, "email = ?")
		args = append(args, *fields.Email)
	}
	if fields.Phone != nil {
		sets = append(sets, "phone = ?")
		args = append(args, *fields.Phone)
	}
	if fields.RealName != nil {
		sets = append(sets, "real_name = ?")
		args = append(args, *fields.RealName)
	}
	if fields.Admin != nil {
		adminInt := 0
		if *fields.Admin {
			adminInt = 1
		}
		sets = append(sets, "admin = ?")
		args = append(args, adminInt)
	}

	if len(sets) == 0 {
		return m.Get(username)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, nowStr)
	args = append(args, username)

	res, err := m.db.Exec(
		fmt.Sprintf("UPDATE accounts SET %s WHERE username = ?", strings.Join(sets, ", ")),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, ErrNotFound
	}

	return m.Get(username)
}

func (m *SQLiteManager) Delete(username string) error {
	res, err := m.db.Exec("DELETE FROM accounts WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	return nil
}

func (m *SQLiteManager) List() ([]Account, error) {
	rows, err := m.db.Query(
		`SELECT username, password_hash, email, phone, real_name, admin, created_at, updated_at
		 FROM accounts ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	var out []Account
	for rows.Next() {
		var acct Account
		var adminInt int
		var createdStr, updatedStr string

		if err := rows.Scan(&acct.Username, &acct.PasswordHash, &acct.Email, &acct.Phone, &acct.RealName, &adminInt, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}

		acct.Admin = adminInt != 0

		acct.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		acct.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}

		out = append(out, acct)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return out, nil
}

func (m *SQLiteManager) Authenticate(username, password string) (*Account, error) {
	acct, err := m.Get(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if !verifyPassword(acct.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	acct.PasswordHash = ""
	return acct, nil
}
