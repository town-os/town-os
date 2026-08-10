package account

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSessionExpired  = errors.New("session expired")
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidToken    = errors.New("invalid token")
)

const SessionMaxAge = 7 * 24 * time.Hour

type Session struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

// SessionManager issues and validates the JWT sessions.
//
// Validate is on the path of every authenticated request, so it is the method
// where the caller's context matters most: it reads the session row AND the
// account row (to check Disabled), and both of those were previously
// uncancellable reads behind the single SQLite connection, on every request
// the box serves.
//
// Cleanup and StartCleanup already took a context — they are the background
// expiry sweep — and are unchanged.
type SessionManager interface {
	Create(ctx context.Context, username string) (token string, err error)
	Validate(ctx context.Context, token string) (*Session, *Account, error)
	Revoke(ctx context.Context, sessionID string) error
	RevokeAllForUser(ctx context.Context, username string) error
	Cleanup(ctx context.Context) error
	List(ctx context.Context, username string) ([]Session, error)
	GetUsername(ctx context.Context, sessionID string) (string, error)
	HasActiveAdminSessions(ctx context.Context, adminUsernames []string) (bool, error)
	StartCleanup(ctx context.Context, interval time.Duration)
}
