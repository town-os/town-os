package account

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLiteNetworkManager struct {
	db *sql.DB
}

func InitNetworkManager(db *sql.DB) (*SQLiteNetworkManager, error) {
	ctx, cancel := dbCtx()
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
		created_at   TEXT NOT NULL,
		PRIMARY KEY (network_name, public_key)
	)`)
	if err != nil {
		return nil, fmt.Errorf("create network_peers table: %w", err)
	}

	return &SQLiteNetworkManager{db: db}, nil
}

func (m *SQLiteNetworkManager) Create(n *Network) (*Network, error) {
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

	ctx, cancel := dbCtx()
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

func (m *SQLiteNetworkManager) Get(name string) (*Network, error) {
	ctx, cancel := dbCtx()
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

func (m *SQLiteNetworkManager) List() (_ []Network, err error) {
	ctx, cancel := dbCtx()
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

func (m *SQLiteNetworkManager) Remove(name string) error {
	if name == DefaultNetworkName {
		return ErrNetworkProtected
	}

	ctx, cancel := dbCtx()
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

func (m *SQLiteNetworkManager) SetEnabled(name string, enabled bool) error {
	ctx, cancel := dbCtx()
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

func (m *SQLiteNetworkManager) Count() (int, error) {
	ctx, cancel := dbCtx()
	defer cancel()

	var count int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM networks").Scan(&count); err != nil {
		return 0, fmt.Errorf("count networks: %w", err)
	}
	return count, nil
}

func (m *SQLiteNetworkManager) AddPeer(p *NetworkPeer) (*NetworkPeer, error) {
	if p == nil || strings.TrimSpace(p.PublicKey) == "" {
		return nil, ErrNetworkPeerKeyReq
	}

	// Ensure the network exists so we return a clean error instead of a
	// foreign-key violation string.
	if _, err := m.Get(p.Network); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	ctx, cancel := dbCtx()
	defer cancel()

	_, err := m.db.ExecContext(ctx,
		`INSERT INTO network_peers (network_name, public_key, name, allowed_ip, endpoint, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.Network, p.PublicKey, p.Name, p.AllowedIP, p.Endpoint, nowStr,
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

func (m *SQLiteNetworkManager) RemovePeer(network, publicKey string) error {
	ctx, cancel := dbCtx()
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

func (m *SQLiteNetworkManager) ListPeers(network string) (_ []NetworkPeer, err error) {
	ctx, cancel := dbCtx()
	defer cancel()

	rows, err := m.db.QueryContext(ctx,
		`SELECT network_name, public_key, name, allowed_ip, endpoint, created_at
		 FROM network_peers WHERE network_name = ? ORDER BY allowed_ip, public_key`, network,
	)
	if err != nil {
		return nil, fmt.Errorf("list network peers: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	out := make([]NetworkPeer, 0, 8)
	for rows.Next() {
		var p NetworkPeer
		var createdStr string
		if err := rows.Scan(&p.Network, &p.PublicKey, &p.Name, &p.AllowedIP, &p.Endpoint, &createdStr); err != nil {
			return nil, fmt.Errorf("scan network peer row: %w", err)
		}
		p.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return out, nil
}
