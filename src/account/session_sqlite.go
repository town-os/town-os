package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type SQLiteSessionManager struct {
	db         *sql.DB
	accountMgr Manager
	signingKey []byte
}

func InitSessionManager(db *sql.DB, mgr Manager, signingKey []byte) (*SQLiteSessionManager, error) {
	_, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		username   TEXT NOT NULL REFERENCES accounts(username) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		last_used  TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create sessions table: %w", err)
	}

	return &SQLiteSessionManager{
		db:         db,
		accountMgr: mgr,
		signingKey: signingKey,
	}, nil
}

func (s *SQLiteSessionManager) Create(username string) (token string, err error) {
	err = s.Cleanup(context.Background())
	if err != nil {
		return "", fmt.Errorf("cleanup expired sessions: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	_, err = s.db.ExecContext(context.Background(),
		`INSERT INTO sessions (id, username, created_at, last_used) VALUES (?, ?, ?, ?)`,
		id, username, nowStr, nowStr,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}

	claims := jwt.MapClaims{
		"session_id": id,
		"username":   username,
		"issued_at":  now.Unix(),
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err = t.SignedString(s.signingKey)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return token, nil
}

func (s *SQLiteSessionManager) Validate(token string) (*Session, *Account, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.signingKey, nil
	})
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return nil, nil, ErrInvalidToken
	}

	sessionID, ok := claims["session_id"].(string)
	if !ok || sessionID == "" {
		return nil, nil, ErrInvalidToken
	}

	var username string
	var createdStr, lastUsedStr string

	err = s.db.QueryRowContext(context.Background(),
		`SELECT username, created_at, last_used FROM sessions WHERE id = ?`, sessionID,
	).Scan(&username, &createdStr, &lastUsedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query session: %w", err)
	}

	lastUsed, err := time.Parse(time.RFC3339, lastUsedStr)
	if err != nil {
		return nil, nil, fmt.Errorf("parse last_used: %w", err)
	}

	if time.Since(lastUsed) > SessionMaxAge {
		_, err := s.db.ExecContext(context.Background(), "DELETE FROM sessions WHERE id = ?", sessionID)
		if err != nil {
			return nil, nil, fmt.Errorf("delete expired session: %w", err)
		}
		return nil, nil, ErrSessionExpired
	}

	createdAt, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return nil, nil, fmt.Errorf("parse created_at: %w", err)
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	_, err = s.db.ExecContext(context.Background(), "UPDATE sessions SET last_used = ? WHERE id = ?", nowStr, sessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("update last_used: %w", err)
	}

	acct, err := s.accountMgr.Get(username)
	if err != nil {
		return nil, nil, err
	}

	sess := &Session{
		ID:        sessionID,
		Username:  username,
		CreatedAt: createdAt,
		LastUsed:  now,
	}

	acct.PasswordHash = ""
	return sess, acct, nil
}

func (s *SQLiteSessionManager) Revoke(sessionID string) error {
	res, err := s.db.ExecContext(context.Background(), "DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SQLiteSessionManager) RevokeAllForUser(username string) error {
	_, err := s.db.ExecContext(context.Background(), "DELETE FROM sessions WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("delete sessions for user: %w", err)
	}
	return nil
}

func (s *SQLiteSessionManager) Cleanup(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-SessionMaxAge).Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE last_used < ?", cutoff)
	if err != nil {
		return fmt.Errorf("cleanup sessions: %w", err)
	}
	return nil
}

func (s *SQLiteSessionManager) List(username string) (_ []Session, err error) {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, username, created_at, last_used FROM sessions WHERE username = ? ORDER BY last_used DESC`,
		username,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var out []Session
	for rows.Next() {
		var sess Session
		var createdStr, lastUsedStr string

		err := rows.Scan(&sess.ID, &sess.Username, &createdStr, &lastUsedStr)
		if err != nil {
			return nil, fmt.Errorf("scan session row: %w", err)
		}

		sess.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		sess.LastUsed, err = time.Parse(time.RFC3339, lastUsedStr)
		if err != nil {
			return nil, fmt.Errorf("parse last_used: %w", err)
		}

		out = append(out, sess)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return out, nil
}

func (s *SQLiteSessionManager) GetUsername(sessionID string) (string, error) {
	var username string
	err := s.db.QueryRowContext(context.Background(), "SELECT username FROM sessions WHERE id = ?", sessionID).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSessionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get username: %w", err)
	}
	return username, nil
}

func (s *SQLiteSessionManager) HasActiveAdminSessions(adminUsernames []string) (bool, error) {
	err := s.Cleanup(context.Background())
	if err != nil {
		return false, fmt.Errorf("cleanup: %w", err)
	}
	if len(adminUsernames) == 0 {
		return false, nil
	}
	placeholders := make([]string, len(adminUsernames))
	args := make([]any, len(adminUsernames))
	for i, u := range adminUsernames {
		placeholders[i] = "?"
		args[i] = u
	}
	query := fmt.Sprintf( //nolint:gosec // placeholders are generated internally, not user input
		"SELECT COUNT(*) FROM sessions WHERE username IN (%s)",
		strings.Join(placeholders, ","),
	)
	var count int
	err = s.db.QueryRowContext(context.Background(), query, args...).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count admin sessions: %w", err)
	}
	return count > 0, nil
}

func (s *SQLiteSessionManager) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := s.Cleanup(ctx)
				if err != nil {
					slog.Error("session cleanup error", "error", err)
				}
			}
		}
	}()
}
