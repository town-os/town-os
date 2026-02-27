package systemcontroller

import (
	"context"
	"errors"

	"gitea.com/town-os/town-os/src/account"
)

func (m *MockClient) Authenticate(_ context.Context, username, password string) (*AuthenticateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Authenticate", Args: []any{username, password}})

	if m.AuthenticateErr != nil {
		return nil, m.AuthenticateErr
	}

	acct, ok := m.Accounts[username]
	if !ok {
		return nil, errors.New("invalid credentials")
	}

	out := *acct
	return &AuthenticateResponse{Token: m.AuthToken, Account: &out}, nil
}

func (m *MockClient) RevokeSession(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RevokeSession", Args: []any{sessionID}})

	if m.RevokeSessionErr != nil {
		return m.RevokeSessionErr
	}

	delete(m.Sessions, sessionID)
	return nil
}

func (m *MockClient) ListSessions(_ context.Context, token string) ([]account.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListSessions", Args: []any{token}})

	if m.ListSessionsErr != nil {
		return nil, m.ListSessionsErr
	}

	var out []account.Session
	for _, sess := range m.Sessions {
		out = append(out, *sess)
	}
	return out, nil
}

func (m *MockClient) SessionUsername(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SessionUsername", Args: []any{token}})

	if m.SessionUsernameErr != nil {
		return "", m.SessionUsernameErr
	}

	for _, sess := range m.Sessions {
		return sess.Username, nil
	}
	return "", errors.New("no sessions")
}
