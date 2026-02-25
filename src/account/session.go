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

type SessionManager interface {
	Create(username string) (token string, err error)
	Validate(token string) (*Session, *Account, error)
	Revoke(sessionID string) error
	RevokeAllForUser(username string) error
	Cleanup(ctx context.Context) error
	List(username string) ([]Session, error)
	GetUsername(sessionID string) (string, error)
	HasActiveAdminSessions(adminUsernames []string) (bool, error)
	StartCleanup(ctx context.Context, interval time.Duration)
}
