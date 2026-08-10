package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/account"
)

// Authenticate validates credentials and returns a session token on success.
// The returned token is a JWT (HS256) with claims for session ID, username,
// and issued timestamp. Sessions expire after 7 days from last use.
//
// Parameters:
//   - username: the account username.
//   - password: the account password (minimum 8 characters).
//
// Calls POST /account/authenticate on the Control Plane Service.
func (c *SystemdClient) Authenticate(ctx context.Context, username, password string) (_ *AuthenticateResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AuthenticateRequest{Username: username, Password: password})

	resp, err := c.postJSON(ctx, "account/authenticate", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: Authenticate: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/authenticate")
	}

	var authResp AuthenticateResponse
	return &authResp, json.NewDecoder(resp.Body).Decode(&authResp)
}

// RevokeSession invalidates a session by its ID, preventing further use of the
// associated token.
//
// Parameters:
//   - sessionID: the UUID of the session to revoke.
//
// Calls POST /account/session/revoke on the Control Plane Service.
func (c *SystemdClient) RevokeSession(ctx context.Context, sessionID string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RevokeSessionRequest{SessionID: sessionID})

	return c.postClient(ctx, "account/session/revoke", pr)
}

// ListSessions returns all active sessions for the user identified by the
// given bearer token. Each session includes ID, username, creation timestamp,
// and last-used timestamp.
//
// Parameters:
//   - token: a valid JWT bearer token identifying the user.
//
// Calls GET /account/sessions on the Control Plane Service.
func (c *SystemdClient) ListSessions(ctx context.Context, token string) (_ []account.Session, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.route("account/sessions"), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: ListSessions: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: ListSessions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "account/sessions")
	}

	var sessions []account.Session
	return sessions, json.NewDecoder(resp.Body).Decode(&sessions)
}

// SessionUsername returns the username associated with the given session token.
//
// Parameters:
//   - token: a valid JWT bearer token.
//
// Calls GET /account/me on the Control Plane Service.
func (c *SystemdClient) SessionUsername(ctx context.Context, token string) (_ string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.route("/account/me"), nil)
	if err != nil {
		return "", fmt.Errorf("%w: SessionUsername: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: SessionUsername: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return "", readProblemDetail(resp, "GET", "/account/me")
	}

	var result SessionUsernameResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	return result.Username, nil
}
