package systemcontroller

import (
	"context"
	"fmt"
	"time"

	"gitea.com/town-os/town-os/src/account"
)

// --- Account ---

func (m *MockClient) CreateAccount(_ context.Context, username, password, email, phone, realName string, admin bool) (*account.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "CreateAccount", Args: []any{username, password, email, phone, realName, admin}})

	if m.CreateAcctErr != nil {
		return nil, m.CreateAcctErr
	}

	now := time.Now()
	acct := &account.Account{
		Username:  username,
		Email:     email,
		Phone:     phone,
		RealName:  realName,
		Admin:     admin,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.Accounts[username] = acct
	out := *acct
	return &out, nil
}

func (m *MockClient) GetAccount(_ context.Context, username string) (*account.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetAccount", Args: []any{username}})

	if m.GetAcctErr != nil {
		return nil, m.GetAcctErr
	}

	acct, ok := m.Accounts[username]
	if !ok {
		return nil, fmt.Errorf("account %s not found", username)
	}
	out := *acct
	return &out, nil
}

func (m *MockClient) UpdateAccount(_ context.Context, username string, fields account.UpdateFields) (*account.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UpdateAccount", Args: []any{username, fields}})

	if m.UpdateAcctErr != nil {
		return nil, m.UpdateAcctErr
	}

	acct, ok := m.Accounts[username]
	if !ok {
		return nil, fmt.Errorf("account %s not found", username)
	}

	if fields.Email != nil {
		acct.Email = *fields.Email
	}
	if fields.Phone != nil {
		acct.Phone = *fields.Phone
	}
	if fields.RealName != nil {
		acct.RealName = *fields.RealName
	}
	if fields.Admin != nil {
		acct.Admin = *fields.Admin
	}
	acct.UpdatedAt = time.Now()

	out := *acct
	return &out, nil
}

func (m *MockClient) DisableAccount(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "DisableAccount", Args: []any{username}})

	if m.DisableAcctErr != nil {
		return m.DisableAcctErr
	}

	acct, ok := m.Accounts[username]
	if !ok {
		return fmt.Errorf("account %s not found", username)
	}
	acct.Disabled = true
	return nil
}

func (m *MockClient) EnableAccount(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "EnableAccount", Args: []any{username}})

	if m.EnableAcctErr != nil {
		return m.EnableAcctErr
	}

	acct, ok := m.Accounts[username]
	if !ok {
		return fmt.Errorf("account %s not found", username)
	}
	acct.Disabled = false
	return nil
}

func (m *MockClient) ListAccounts(_ context.Context, params ListParams) (*PageResult[account.Account], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListAccounts", Args: []any{params}})

	if m.ListAcctErr != nil {
		return nil, m.ListAcctErr
	}

	var out []account.Account
	for _, acct := range m.Accounts {
		out = append(out, *acct)
	}
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}
