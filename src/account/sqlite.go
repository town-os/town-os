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
	"sort"
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
		grants        TEXT NOT NULL DEFAULT '[]',
		networks      TEXT NOT NULL DEFAULT '[]',
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create accounts table: %w", err)
	}

	// Migrate pre-existing accounts tables that predate the network-only
	// account kind. The DEFAULTs make every existing row read back as a plain
	// account with no network scope, so the migration is safe to run against any
	// older database. A duplicate column error means it already ran.
	for _, col := range []struct{ name, def string }{
		{"grants", "TEXT NOT NULL DEFAULT '[]'"},
		{"networks", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE accounts ADD COLUMN %s %s", col.name, col.def),
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate accounts.%s: %w", col.name, err)
		}
	}

	if err := migrateLegacyAccountColumns(ctx, db); err != nil {
		return nil, err
	}

	return &SQLiteManager{db: db}, nil
}

// legacyGrantColumns are boolean capability columns earlier releases wrote,
// paired with the grant each one becomes.
//
// The capabilities did not change, only their representation: a column per
// capability became a named set, so every one of these is carried forward
// rather than dropped. An account that could enroll peers still can.
var legacyGrantColumns = map[string]string{
	"wireguard":      GrantWireGuard,
	"object_storage": GrantGfeh,
	// An in-flight schema that briefly folded both capabilities into one flag.
	// It granted both, so it restores both -- anything narrower would silently
	// take authority away from an account on upgrade.
	"network_only": GrantWireGuard + "," + GrantGfeh,
}

// smb_nt_hash held MD4(UTF16LE(password)) for the SMB view. No account carries
// an SMB password now and no partition serves the view, so the hash buys
// nothing -- while remaining the weakest secret at rest in the database:
// unsalted, no work factor, and password-equivalent to anything that still
// speaks NTLM. An upgraded box keeps every hash it ever enrolled unless it is
// dropped here.
var legacyAccountColumns = []string{"smb_nt_hash"}

// migrateLegacyAccountColumns carries every legacy capability column into the
// grant set and drops the columns listed above.
//
// Guarded by column lookups rather than by matching the error text SQLite
// returns for an absent column: that message is not an API, and this path runs
// on every boot of every box, including fresh ones whose CREATE TABLE never had
// any of these columns.
func migrateLegacyAccountColumns(ctx context.Context, db *sql.DB) error {
	// Each capability column becomes its grant before the column goes. Sorted
	// so the order is a function of the schema rather than of map iteration:
	// an account holding two of them must end up with the same stored set on
	// every box, or the rows differ byte-wise for no reason.
	columns := make([]string, 0, len(legacyGrantColumns))
	for col := range legacyGrantColumns {
		columns = append(columns, col)
	}
	sort.Strings(columns)

	for _, col := range columns {
		present, err := columnExists(ctx, db, col)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if err := carryGrantColumn(ctx, db, col, strings.Split(legacyGrantColumns[col], ",")); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE accounts DROP COLUMN "+col); err != nil {
			return fmt.Errorf("drop accounts.%s: %w", col, err)
		}
	}

	for _, col := range legacyAccountColumns {
		present, err := columnExists(ctx, db, col)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE accounts DROP COLUMN "+col); err != nil {
			return fmt.Errorf("drop accounts.%s: %w", col, err)
		}
	}
	return nil
}

// carryGrantColumn adds grants to every account whose legacy capability column
// is set.
//
// Read-modify-write per row rather than a SQL update, because the target is a
// JSON array and the merge has to de-duplicate: two legacy columns can map to
// the same grant, and a box that crashed mid-migration re-runs this.
func carryGrantColumn(ctx context.Context, db *sql.DB, column string, grants []string) (err error) {
	rows, err := db.QueryContext(ctx, "SELECT username, grants FROM accounts WHERE "+column+" = 1") //nolint:gosec // G202 -- column comes from legacyGrantColumns, never from a caller
	if err != nil {
		return fmt.Errorf("select accounts by %s: %w", column, err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	held := map[string][]string{}
	for rows.Next() {
		var username, stored string
		if err := rows.Scan(&username, &stored); err != nil {
			return fmt.Errorf("scan %s row: %w", column, err)
		}
		existing, err := unmarshalNetworks(stored)
		if err != nil {
			return fmt.Errorf("decode grants for %s: %w", username, err)
		}
		held[username] = normalizeGrants(append(existing, grants...))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate accounts by %s: %w", column, err)
	}

	for username, set := range held {
		encoded, err := marshalNetworks(set)
		if err != nil {
			return fmt.Errorf("encode grants for %s: %w", username, err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE accounts SET grants = ? WHERE username = ?", encoded, username); err != nil {
			return fmt.Errorf("carry accounts.%s into grants for %s: %w", column, username, err)
		}
	}
	return nil
}

// columnExists reports whether the accounts table carries a named column.
//
// Scoped to one table rather than taking the name as a parameter: PRAGMA
// table_info cannot be parameterised, so a table name would have to be
// interpolated into the statement, and the only way to be sure that is never a
// caller's input is not to accept one.
func columnExists(ctx context.Context, db *sql.DB, column string) (_ bool, err error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(accounts)")
	if err != nil {
		return false, fmt.Errorf("table_info accounts: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	found := false
	for rows.Next() {
		var (
			cid           int
			name, colType string
			notNull, pk   int
			dflt          sql.NullString
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("scan table_info accounts: %w", err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table_info accounts: %w", err)
	}
	return found, nil
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

// Create makes an ordinary account -- an administrator or a plain dashboard
// user -- holding no explicit grants and belonging to the home network.
//
// The home network is not a confinement here, and that distinction is the whole
// reason it is safe to write. The scope column confines only a *restricted*
// account (a non-admin holding grants); for an account with none it is
// membership, which is why it is written on this path and never merged into
// CreateGranted's scope -- there, the set an administrator chose is exactly the
// networks the account may reach, and quietly adding home to it would widen a
// portal account scoped to `office` into the home network.
//
// account.DefaultNetwork() always exists (InitNetworkManager seeds it), so this
// never names a network that is not there -- including for the very first
// account, created before boot reconcile has run.
func (m *SQLiteManager) Create(username, password, email, phone, realName string, admin bool) (*Account, error) {
	return m.create(username, password, email, phone, realName, admin, nil, []string{DefaultNetworkName})
}

// CreateGranted creates a non-admin account holding grants. It can never be
// admin (an administrator holds every grant already, so a stored subset is
// incoherent), every grant is checked against AllGrants, and the network scope
// is validated non-empty before the row is written -- so an account holding a
// grant with no reachable network cannot exist.
func (m *SQLiteManager) CreateGranted(username, password, email, phone, realName string, grants, networks []string) (*Account, error) {
	if err := validateGrants(grants); err != nil {
		return nil, err
	}
	if err := validateNetworkScope(networks); err != nil {
		return nil, err
	}
	return m.create(username, password, email, phone, realName, false, normalizeGrants(grants), normalizeNetworkScope(networks))
}

// create is the shared insert path. Keeping it in one place means the grant set
// and network scope are written atomically with the rest of the row — there is
// no window where an account exists but its authority has not been applied yet.
func (m *SQLiteManager) create(username, password, email, phone, realName string, admin bool, grants, networks []string) (_ *Account, err error) {
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

	grantsJSON, err := marshalNetworks(grants)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	ctx, cancel := dbCtx()
	defer cancel()

	_, err = m.db.ExecContext(ctx,
		`INSERT INTO accounts (username, password_hash, email, phone, real_name, admin, disabled, grants, networks, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		username, hash, email, phone, realName, admin, grantsJSON, networksJSON, nowStr, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrDuplicateUsername
		}
		return nil, fmt.Errorf("insert account: %w", err)
	}

	return &Account{
		Username:    username,
		Email:       email,
		Phone:       phone,
		RealName:    realName,
		Admin:    admin,
		Grants:   grants,
		Networks: networks,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// accountColumns is the canonical SELECT column list for an account, shared by
// Get and List so the two can never drift out of sync with scanAccountRow.
const accountColumns = `username, password_hash, email, phone, real_name, admin, disabled, grants, networks, created_at, updated_at`

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
	var createdStr, updatedStr, grantsJSON, networksJSON string

	err := s.Scan(
		&acct.Username, &acct.PasswordHash, &acct.Email, &acct.Phone, &acct.RealName,
		&acct.Admin, &acct.Disabled, &grantsJSON, &networksJSON,
		&createdStr, &updatedStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("scan account: %w", err)
	}

	acct.Grants, err = unmarshalNetworks(grantsJSON)
	if err != nil {
		return nil, err
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
	if fields.Grants != nil {
		grantsJSON, err := marshalNetworks(normalizeGrants(*fields.Grants))
		if err != nil {
			return nil, err
		}
		sets = append(sets, "grants = ?")
		args = append(args, grantsJSON)
	}
	if fields.Networks != nil {
		networksJSON, err := marshalNetworks(normalizeNetworkScope(*fields.Networks))
		if err != nil {
			return nil, err
		}
		sets = append(sets, "networks = ?")
		args = append(args, networksJSON)
	}
	// Guard both grant invariants across the *resulting* row, not just this
	// request: adding a grant while leaving Networks empty (or clearing
	// Networks out from under an existing grant) leaves authority with no
	// reachable network, and grants stored on an administrator can only
	// disagree with the every-grant rule. Resolve the post-update state and
	// reject it before writing.
	if err := m.validateGrantResult(username, fields); err != nil {
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

// validateGrantResult enforces the two grant invariants against the state the
// row will have *after* the update: an administrator holds no explicit grants,
// and an account that holds any is scoped to at least one network.
//
// It reads the current account and overlays whichever of Grants, Admin, and
// Networks the update sets, so every path into a bad row is caught — granting
// an administrator, promoting a grant-holder, or clearing the scope out from
// under a grant — rather than only the paths where the offending field is the
// one being written.
func (m *SQLiteManager) validateGrantResult(username string, fields UpdateFields) error {
	if fields.Grants == nil && fields.Networks == nil && fields.Admin == nil {
		return nil // no dimension that matters is changing
	}

	current, err := m.Get(username)
	if err != nil {
		return err
	}

	grants := current.Grants
	if fields.Grants != nil {
		grants = *fields.Grants
	}
	admin := current.Admin
	if fields.Admin != nil {
		admin = *fields.Admin
	}
	if len(grants) > 0 && admin {
		return ErrGrantsAdmin
	}
	if len(grants) == 0 {
		return nil // any account may have an empty (including absent) scope
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
