package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLiteNetworkManager struct {
	db *sql.DB
}

func InitNetworkManager(ctx context.Context, db *sql.DB) (*SQLiteNetworkManager, error) {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS networks (
		name         TEXT PRIMARY KEY,
		tld          TEXT NOT NULL,
		subnet       TEXT NOT NULL DEFAULT '',
		address      TEXT NOT NULL DEFAULT '',
		public_key   TEXT NOT NULL DEFAULT '',
		private_key  TEXT NOT NULL DEFAULT '',
		listen_port  INTEGER NOT NULL DEFAULT 0,
		enabled      INTEGER NOT NULL DEFAULT 1,
		created_at   TEXT NOT NULL,
		updated_at   TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create networks table: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS network_peers (
		network_name TEXT NOT NULL REFERENCES networks(name) ON DELETE CASCADE,
		public_key   TEXT NOT NULL,
		name         TEXT NOT NULL DEFAULT '',
		allowed_ip   TEXT NOT NULL DEFAULT '',
		endpoint     TEXT NOT NULL DEFAULT '',
		rolodex      INTEGER NOT NULL DEFAULT 0,
		created_by   TEXT NOT NULL DEFAULT '',
		expires_at   TEXT,
		created_at   TEXT NOT NULL,
		PRIMARY KEY (network_name, public_key)
	)`)
	if err != nil {
		return nil, fmt.Errorf("create network_peers table: %w", err)
	}

	// Migrate pre-existing network_peers tables that lack the newer columns. A
	// duplicate-column error means that column's migration already ran; ignore
	// it. expires_at is deliberately nullable (no NOT NULL/DEFAULT): a NULL means
	// "never expires", which is exactly how every peer that predates the TTL
	// feature must keep behaving.
	for _, col := range []struct{ name, def string }{
		{"rolodex", "INTEGER NOT NULL DEFAULT 0"},
		{"created_by", "TEXT NOT NULL DEFAULT ''"},
		{"expires_at", "TEXT"},
	} {
		if _, err = db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE network_peers ADD COLUMN %s %s", col.name, col.def),
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate network_peers.%s: %w", col.name, err)
		}
	}

	m := &SQLiteNetworkManager{db: db}
	if _, err := m.Create(ctx, DefaultNetwork()); err != nil && !errors.Is(err, ErrDuplicateNetwork) {
		return nil, fmt.Errorf("seed default network: %w", err)
	}
	return m, nil
}

func (m *SQLiteNetworkManager) Create(ctx context.Context, n *Network) (*Network, error) {
	if n == nil {
		return nil, ErrNetworkNameRequired
	}
	if strings.TrimSpace(n.Name) == "" {
		return nil, ErrNetworkNameRequired
	}
	if !ValidNetworkName(n.Name) {
		return nil, ErrNetworkNameInvalid
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	ctx, cancel := queryCtx(ctx)
	defer cancel()

	_, err := m.db.ExecContext(ctx,
		`INSERT INTO networks (name, tld, subnet, address, public_key, private_key, listen_port, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, n.TLD, n.Subnet, n.Address, n.PublicKey, n.PrivateKey, n.ListenPort, n.Enabled, nowStr, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrDuplicateNetwork
		}
		return nil, fmt.Errorf("insert network: %w", err)
	}

	out := *n
	out.CreatedAt = now
	out.UpdatedAt = now
	return &out, nil
}

func (m *SQLiteNetworkManager) Get(ctx context.Context, name string) (*Network, error) {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	row := m.db.QueryRowContext(ctx,
		`SELECT name, tld, subnet, address, public_key, private_key, listen_port, enabled, created_at, updated_at
		 FROM networks WHERE name = ?`, name,
	)
	return scanNetwork(row)
}

func scanNetwork(row *sql.Row) (*Network, error) {
	var n Network
	var createdStr, updatedStr string
	err := row.Scan(&n.Name, &n.TLD, &n.Subnet, &n.Address, &n.PublicKey, &n.PrivateKey, &n.ListenPort, &n.Enabled, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, ErrNetworkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan network: %w", err)
	}

	var parseErr error
	n.CreatedAt, parseErr = time.Parse(time.RFC3339, createdStr)
	if parseErr != nil {
		return nil, fmt.Errorf("parse created_at: %w", parseErr)
	}
	n.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedStr)
	if parseErr != nil {
		return nil, fmt.Errorf("parse updated_at: %w", parseErr)
	}
	return &n, nil
}

func (m *SQLiteNetworkManager) List(ctx context.Context) (_ []Network, err error) {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	rows, err := m.db.QueryContext(ctx,
		`SELECT name, tld, subnet, address, public_key, private_key, listen_port, enabled, created_at, updated_at
		 FROM networks ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	out := make([]Network, 0, 8)
	for rows.Next() {
		var n Network
		var createdStr, updatedStr string
		if err := rows.Scan(&n.Name, &n.TLD, &n.Subnet, &n.Address, &n.PublicKey, &n.PrivateKey, &n.ListenPort, &n.Enabled, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan network row: %w", err)
		}
		n.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		n.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return out, nil
}

func (m *SQLiteNetworkManager) Remove(ctx context.Context, name string) error {
	if name == DefaultNetworkName {
		return ErrNetworkProtected
	}

	ctx, cancel := queryCtx(ctx)
	defer cancel()

	res, err := m.db.ExecContext(ctx, "DELETE FROM networks WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete network: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNetworkNotFound
	}
	return nil
}

func (m *SQLiteNetworkManager) SetEnabled(ctx context.Context, name string, enabled bool) error {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	nowStr := time.Now().UTC().Format(time.RFC3339)
	res, err := m.db.ExecContext(ctx,
		"UPDATE networks SET enabled = ?, updated_at = ? WHERE name = ?", enabled, nowStr, name)
	if err != nil {
		return fmt.Errorf("set network enabled: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNetworkNotFound
	}
	return nil
}

func (m *SQLiteNetworkManager) SetTLD(ctx context.Context, name, tld string) error {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	nowStr := time.Now().UTC().Format(time.RFC3339)
	res, err := m.db.ExecContext(ctx,
		"UPDATE networks SET tld = ?, updated_at = ? WHERE name = ?", tld, nowStr, name)
	if err != nil {
		return fmt.Errorf("set network tld: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNetworkNotFound
	}
	return nil
}

func (m *SQLiteNetworkManager) Count(ctx context.Context) (int, error) {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	var count int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM networks").Scan(&count); err != nil {
		return 0, fmt.Errorf("count networks: %w", err)
	}
	return count, nil
}

func (m *SQLiteNetworkManager) AddPeer(ctx context.Context, p *NetworkPeer) (*NetworkPeer, error) {
	if p == nil || strings.TrimSpace(p.PublicKey) == "" {
		return nil, ErrNetworkPeerKeyReq
	}

	// The home network is DNS-only: it carries no subnet, no keypair, and no
	// interface, so a peer row on it describes a tunnel that does not exist and
	// never will. Refused at the manager, not only at the API, because the row
	// would otherwise be reachable by any caller holding a NetworkManager --
	// and because the API's own guard is the kind that gets moved.
	if p.Network == DefaultNetworkName {
		return nil, ErrNetworkDNSOnly
	}

	// Ensure the network exists so we return a clean error instead of a
	// foreign-key violation string.
	if _, err := m.Get(ctx, p.Network); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	ctx, cancel := queryCtx(ctx)
	defer cancel()

	rolodex := 0
	if p.Rolodex {
		rolodex = 1
	}
	// A nil ExpiresAt is stored as SQL NULL ("never expires"); a non-nil one is
	// normalized to UTC RFC3339 so lexical <= comparison in the reaper matches
	// chronological order, exactly as created_at does.
	var expiresVal any
	if p.ExpiresAt != nil {
		expiresVal = p.ExpiresAt.UTC().Format(time.RFC3339)
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO network_peers (network_name, public_key, name, allowed_ip, endpoint, rolodex, created_by, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Network, p.PublicKey, p.Name, p.AllowedIP, p.Endpoint, rolodex, p.CreatedBy, expiresVal, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return nil, ErrDuplicateNetworkPeer
		}
		return nil, fmt.Errorf("insert network peer: %w", err)
	}

	out := *p
	out.CreatedAt = now
	return &out, nil
}

func (m *SQLiteNetworkManager) RemovePeer(ctx context.Context, network, publicKey string) error {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	res, err := m.db.ExecContext(ctx,
		"DELETE FROM network_peers WHERE network_name = ? AND public_key = ?", network, publicKey)
	if err != nil {
		return fmt.Errorf("delete network peer: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNetworkPeerNotFound
	}
	return nil
}

// networkPeerColumns is the canonical SELECT column list for a peer, shared by
// ListPeers and ReapExpiredPeers so the two can never drift out of sync with
// scanNetworkPeerRow.
const networkPeerColumns = `network_name, public_key, name, allowed_ip, endpoint, rolodex, created_by, expires_at, created_at`

// scanNetworkPeerRow reads one peer, decoding the nullable expires_at into a
// *time.Time (nil for a NULL / "never expires" row). It is fed from *sql.Rows in
// both list and reap paths via the shared rowScanner interface.
func scanNetworkPeerRow(s rowScanner) (*NetworkPeer, error) {
	var p NetworkPeer
	var createdStr string
	var expiresStr sql.NullString
	var rolodex int
	if err := s.Scan(&p.Network, &p.PublicKey, &p.Name, &p.AllowedIP, &p.Endpoint, &rolodex, &p.CreatedBy, &expiresStr, &createdStr); err != nil {
		return nil, fmt.Errorf("scan network peer row: %w", err)
	}
	p.Rolodex = rolodex != 0

	var err error
	p.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if expiresStr.Valid && strings.TrimSpace(expiresStr.String) != "" {
		expires, err := time.Parse(time.RFC3339, expiresStr.String)
		if err != nil {
			return nil, fmt.Errorf("parse expires_at: %w", err)
		}
		p.ExpiresAt = &expires
	}
	return &p, nil
}

func (m *SQLiteNetworkManager) ListPeers(ctx context.Context, network string) (_ []NetworkPeer, err error) {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	rows, err := m.db.QueryContext(ctx,
		"SELECT "+networkPeerColumns+" FROM network_peers WHERE network_name = ? ORDER BY allowed_ip, public_key", network,
	)
	if err != nil {
		return nil, fmt.Errorf("list network peers: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	out := make([]NetworkPeer, 0, 8)
	for rows.Next() {
		p, err := scanNetworkPeerRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return out, nil
}

// RefreshPeer extends a peer's expiry. It sets expires_at unconditionally to the
// supplied time, so a heartbeat both slides a TTL'd peer forward and (were it
// ever called on a permanent peer) would give it one — callers pass now+peer_ttl.
func (m *SQLiteNetworkManager) RefreshPeer(ctx context.Context, network, publicKey string, expiresAt time.Time) error {
	ctx, cancel := queryCtx(ctx)
	defer cancel()

	res, err := m.db.ExecContext(ctx,
		"UPDATE network_peers SET expires_at = ? WHERE network_name = ? AND public_key = ?",
		expiresAt.UTC().Format(time.RFC3339), network, publicKey,
	)
	if err != nil {
		return fmt.Errorf("refresh network peer: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNetworkPeerNotFound
	}
	return nil
}

// ReapExpiredPeers removes every peer whose expiry has passed and returns the
// removed set. The select and delete run in one transaction so a concurrent
// RefreshPeer cannot slip a peer's expiry forward between the two statements and
// have it both survive and be reported reaped (the single-connection pool would
// otherwise release the connection between statements).
func (m *SQLiteNetworkManager) ReapExpiredPeers(ctx context.Context, now time.Time) (_ []NetworkPeer, err error) {
	nowStr := now.UTC().Format(time.RFC3339)

	ctx, cancel := queryCtx(ctx)
	defer cancel()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reap transaction: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	// The rows must be fully drained and closed before the DELETE runs on the
	// same single-connection transaction, so the select lives in its own helper
	// whose deferred Close fires before we return here to delete.
	expired, err := selectExpiredPeers(ctx, tx, nowStr)
	if err != nil {
		return nil, err
	}

	if len(expired) > 0 {
		if _, err = tx.ExecContext(ctx,
			"DELETE FROM network_peers WHERE expires_at IS NOT NULL AND expires_at <= ?", nowStr,
		); err != nil {
			return nil, fmt.Errorf("delete expired peers: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reap transaction: %w", err)
	}
	return expired, nil
}

func selectExpiredPeers(ctx context.Context, tx *sql.Tx, nowStr string) (_ []NetworkPeer, err error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT "+networkPeerColumns+" FROM network_peers WHERE expires_at IS NOT NULL AND expires_at <= ? ORDER BY network_name, public_key",
		nowStr,
	)
	if err != nil {
		return nil, fmt.Errorf("select expired peers: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var expired []NetworkPeer
	for rows.Next() {
		p, scanErr := scanNetworkPeerRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		expired = append(expired, *p)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("rows iteration: %w", rowsErr)
	}
	return expired, nil
}
