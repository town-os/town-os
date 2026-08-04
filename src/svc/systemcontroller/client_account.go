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

// --- Account ---

// CreateAccount creates a new user account with the given profile fields.
// When admin is true the account receives administrator privileges.
func (c *SystemdClient) CreateAccount(ctx context.Context, username, password, email, phone, realName string, admin bool) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreateAccountRequest{
		Username: username, Password: password,
		Email: email, Phone: phone, RealName: realName, Admin: admin,
	})

	resp, err := c.postJSON(ctx, "account/create", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: CreateAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/create")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// CreateGrantedAccount creates a non-admin account holding grants, scoped to
// networks. It may reach only the routes those grants unlock, on those networks.
func (c *SystemdClient) CreateGrantedAccount(ctx context.Context, username, password, email, phone, realName string, grants, networks []string) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreateAccountRequest{
		Username: username, Password: password,
		Email: email, Phone: phone, RealName: realName,
		Grants: grants, Networks: networks,
	})

	resp, err := c.postJSON(ctx, "account/create", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: CreateGrantedAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/create")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// GetAccount retrieves a user account by username.
func (c *SystemdClient) GetAccount(ctx context.Context, username string) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetAccountRequest{Username: username})

	resp, err := c.postJSON(ctx, "account", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// UpdateAccount modifies fields on an existing user account. Only non-nil
// fields in the [account.UpdateFields] struct are applied.
func (c *SystemdClient) UpdateAccount(ctx context.Context, username string, fields account.UpdateFields) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UpdateAccountRequest{Username: username, Fields: fields})

	resp, err := c.postJSON(ctx, "account/update", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: UpdateAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/update")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// DisableAccount prevents the named user from authenticating.
func (c *SystemdClient) DisableAccount(ctx context.Context, username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, DisableAccountRequest{Username: username})

	return c.postClient(ctx, "account/disable", pr)
}

// EnableAccount re-enables a previously disabled user account.
func (c *SystemdClient) EnableAccount(ctx context.Context, username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, EnableAccountRequest{Username: username})

	return c.postClient(ctx, "account/enable", pr)
}

// ListAccounts returns a paginated list of all user accounts.
func (c *SystemdClient) ListAccounts(ctx context.Context, params ListParams) (_ *PageResult[account.Account], err error) {
	resp, err := c.getClient(ctx, "account"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListAccounts: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "account")
	}

	var page PageResult[account.Account]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}
