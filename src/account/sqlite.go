package account

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

// dbTimeout is the default timeout for SQLite database operations.
const dbTimeout = 30 * time.Second

// dbCtx returns a context with the default database timeout.
func dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), dbTimeout)
}

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
	defer func() {
		if err != nil {
			err = errors.Join(err, db.Close())
			db = nil
		}
	}()

	// SQLite only allows one writer at a time. Limiting to a single
	// connection ensures all PRAGMAs apply consistently and eliminates
	// SQLITE_BUSY errors from connection pool contention.
	db.SetMaxOpenConns(1)

	pragmaCtx, pragmaCancel := dbCtx()
	defer pragmaCancel()

	_, err = db.ExecContext(pragmaCtx, "PRAGMA journal_mode=WAL")
	if err != nil {
		return db, fmt.Errorf("pragma WAL: %w", err)
	}

	_, err = db.ExecContext(pragmaCtx, "PRAGMA busy_timeout=5000")
	if err != nil {
		return db, fmt.Errorf("pragma busy_timeout: %w", err)
	}

	_, err = db.ExecContext(pragmaCtx, "PRAGMA foreign_keys=ON")
	if err != nil {
		return db, fmt.Errorf("pragma foreign_keys: %w", err)
	}

	return db, nil
}

func InitManager(db *sql.DB) (*SQLiteManager, error) {
	ctx, cancel := dbCtx()
	defer cancel()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS accounts (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		email         TEXT NOT NULL,
		phone         TEXT NOT NULL,
		real_name     TEXT NOT NULL,
		admin         INTEGER NOT NULL DEFAULT 0,
		disabled      INTEGER NOT NULL DEFAULT 0,
		wireguard     INTEGER NOT NULL DEFAULT 0,
		networks      TEXT NOT NULL DEFAULT '[]',
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create accounts table: %w", err)
	}

	// Migrate pre-existing accounts tables that predate the WireGuard-only
	// account type. The DEFAULTs make every existing row read back exactly as
	// it did before (a normal, non-WireGuard account with no network scope), so
	// the migration is safe to run against any older database. A duplicate
	// column error means it already ran.
	for _, col := range []struct{ name, def string }{
		{"wireguard", "INTEGER NOT NULL DEFAULT 0"},
		{"networks", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE accounts ADD COLUMN %s %s", col.name, col.def),
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate accounts.%s: %w", col.name, err)
		}
	}

	return &SQLiteManager{db: db}, nil
}

// marshalNetworks encodes a network scope for storage. An empty or nil scope is
// stored as the JSON empty array so the column is never NULL and always parses.
func marshalNetworks(networks []string) (string, error) {
	if len(networks) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(networks)
	if err != nil {
		return "", fmt.Errorf("encode network scope: %w", err)
	}
	return string(b), nil
}

// unmarshalNetworks decodes a stored network scope. A blank value (possible on
// a row written before the column existed and then read oddly) decodes to nil
// rather than erroring.
func unmarshalNetworks(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("decode network scope %q: %w", s, err)
	}
	return out, nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	_, err := rand.Read(salt)
	if err != nil {
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
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads)
	if err != nil {
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

	keyLen := len(expectedKey)
	if keyLen > math.MaxUint32 {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(keyLen))

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

func (m *SQLiteManager) Create(username, password, email, phone, realName string, admin bool) (*Account, error) {
	return m.create(username, password, email, phone, realName, admin, false, nil)
}

// CreateWireGuard creates a WireGuard-only account. It can never be admin (the
// two are mutually exclusive by construction here, not by a caller's
// discipline), and its network scope is validated non-empty before the row is
// written, so a WireGuard account with no reachable network cannot exist.
func (m *SQLiteManager) CreateWireGuard(username, password, email, phone, realName string, networks []string) (*Account, error) {
	if err := validateNetworkScope(networks); err != nil {
		return nil, err
	}
	return m.create(username, password, email, phone, realName, false, true, normalizeNetworkScope(networks))
}

// create is the shared insert path for both account kinds. Keeping it in one
// place means the WireGuard flag and network scope are written atomically with
// the rest of the row — there is no window where an account exists but its
// restriction has not been applied yet.
func (m *SQLiteManager) create(username, password, email, phone, realName string, admin, wireguard bool, networks []string) (_ *Account, err error) {
	if err = validatePassword(password); err != nil {
		return nil, err
	}
	if err = validateContactInfo(email, phone, realName); err != nil {
		return nil, err
	}

	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	networksJSON, err := marshalNetworks(networks)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	ctx, cancel := dbCtx()
	defer cancel()

	_, err = m.db.ExecContext(ctx,
		`INSERT INTO accounts (username, password_hash, email, phone, real_name, admin, disabled, wireguard, networks, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		username, hash, email, phone, realName, admin, wireguard, networksJSON, nowStr, nowStr,
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
		WireGuard: wireguard,
		Networks:  networks,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// accountColumns is the canonical SELECT column list for an account, shared by
// Get and List so the two can never drift out of sync with scanAccountRow.
const accountColumns = `username, password_hash, email, phone, real_name, admin, disabled, wireguard, networks, created_at, updated_at`

func (m *SQLiteManager) Get(username string) (*Account, error) {
	ctx, cancel := dbCtx()
	defer cancel()

	row := m.db.QueryRowContext(ctx,
		"SELECT "+accountColumns+" FROM accounts WHERE username = ?", username,
	)
	acct, err := scanAccountRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return acct, err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, so scanAccountRow can
// serve Get and List from one implementation.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanAccountRow reads one account, including the WireGuard flag and network
// scope. It returns sql.ErrNoRows unwrapped so the caller can map it (Get →
// ErrNotFound; List treats it as end-of-rows, which never occurs there).
func scanAccountRow(s rowScanner) (*Account, error) {
	var acct Account
	var createdStr, updatedStr, networksJSON string

	err := s.Scan(
		&acct.Username, &acct.PasswordHash, &acct.Email, &acct.Phone, &acct.RealName,
		&acct.Admin, &acct.Disabled, &acct.WireGuard, &networksJSON, &createdStr, &updatedStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("scan account: %w", err)
	}

	acct.Networks, err = unmarshalNetworks(networksJSON)
	if err != nil {
		return nil, err
	}

	acct.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	acct.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &acct, nil
}

func (m *SQLiteManager) Update(username string, fields UpdateFields) (_ *Account, err error) {
	err = validateUpdateFields(fields)
	if err != nil {
		return nil, err
	}

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
		sets = append(sets, "admin = ?")
		args = append(args, *fields.Admin)
	}
	if fields.WireGuard != nil {
		sets = append(sets, "wireguard = ?")
		args = append(args, *fields.WireGuard)
	}
	if fields.Networks != nil {
		networksJSON, err := marshalNetworks(normalizeNetworkScope(*fields.Networks))
		if err != nil {
			return nil, err
		}
		sets = append(sets, "networks = ?")
		args = append(args, networksJSON)
	}

	// Guard the WireGuard invariant across the *resulting* row, not just this
	// request: turning WireGuard on while leaving Networks empty (or clearing
	// Networks on an account that is, or is becoming, WireGuard) would leave a
	// restricted account with no reachable network. Resolve the post-update
	// state and reject it before writing.
	if err := m.validateWireGuardResult(username, fields); err != nil {
		return nil, err
	}

	if len(sets) == 0 {
		return m.Get(username)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, nowStr)
	args = append(args, username)

	ctx, cancel := dbCtx()
	defer cancel()

	res, err := m.db.ExecContext(ctx,
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

// validateWireGuardResult enforces "a WireGuard account always has a non-empty
// network scope" against the state the row will have *after* the update. It
// reads the current account and overlays whichever of WireGuard/Networks the
// update sets, so every path into an empty-scoped WireGuard account is caught:
// enabling WireGuard without networks, or clearing networks on a WireGuard
// account.
func (m *SQLiteManager) validateWireGuardResult(username string, fields UpdateFields) error {
	if fields.WireGuard == nil && fields.Networks == nil {
		return nil // neither dimension is changing
	}

	current, err := m.Get(username)
	if err != nil {
		return err
	}

	wireguard := current.WireGuard
	if fields.WireGuard != nil {
		wireguard = *fields.WireGuard
	}
	if !wireguard {
		return nil // a non-WireGuard account may have any (including empty) scope
	}

	networks := current.Networks
	if fields.Networks != nil {
		networks = *fields.Networks
	}
	return validateNetworkScope(networks)
}

func (m *SQLiteManager) Disable(username string) error {
	ctx, cancel := dbCtx()
	defer cancel()

	nowStr := time.Now().UTC().Format(time.RFC3339)
	res, err := m.db.ExecContext(ctx, "UPDATE accounts SET disabled = 1, updated_at = ? WHERE username = ?", nowStr, username)
	if err != nil {
		return fmt.Errorf("disable account: %w", err)
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

func (m *SQLiteManager) Enable(username string) error {
	ctx, cancel := dbCtx()
	defer cancel()

	nowStr := time.Now().UTC().Format(time.RFC3339)
	res, err := m.db.ExecContext(ctx, "UPDATE accounts SET disabled = 0, updated_at = ? WHERE username = ?", nowStr, username)
	if err != nil {
		return fmt.Errorf("enable account: %w", err)
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
	ctx, cancel := dbCtx()
	defer cancel()

	rows, err := m.db.QueryContext(ctx,
		"SELECT "+accountColumns+" FROM accounts ORDER BY username",
	)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var out []Account
	for rows.Next() {
		acct, err := scanAccountRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *acct)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return out, nil
}

func (m *SQLiteManager) Authenticate(username, password string) (*Account, error) {
	acct, err := m.Get(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if acct.Disabled {
		return nil, ErrAccountDisabled
	}

	if !verifyPassword(acct.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	acct.PasswordHash = ""
	return acct, nil
}
