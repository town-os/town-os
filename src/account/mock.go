package account

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MockCall struct {
	Method string
	Args   []any
}

// --- MockManager ---

type MockManager struct {
	mu       sync.Mutex
	accounts map[string]*Account
	Calls    []MockCall

	CreateErr       error
	GetErr          error
	UpdateErr       error
	DisableErr      error
	EnableErr       error
	ListErr         error
	AuthenticateErr error
}

func InitMockManager() *MockManager {
	return &MockManager{
		accounts: map[string]*Account{},
	}
}

func (m *MockManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

// Create mirrors SQLiteManager.Create, including the home-network membership:
// an account that came back scoped only against the real store would make every
// test over the mock disagree with the box.
func (m *MockManager) Create(username, password, email, phone, realName string, admin bool) (*Account, error) {
	return m.create("Create", username, password, email, phone, realName, admin, nil, []string{DefaultNetworkName})
}

func (m *MockManager) CreateGranted(username, password, email, phone, realName string, grants, networks []string) (*Account, error) {
	if err := validateGrants(grants); err != nil {
		return nil, err
	}
	if err := validateNetworkScope(networks); err != nil {
		return nil, err
	}
	return m.create("CreateGranted", username, password, email, phone, realName, false, normalizeGrants(grants), normalizeNetworkScope(networks))
}

func (m *MockManager) create(method, username, password, email, phone, realName string, admin bool, grants, networks []string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: []any{username, password, email, phone, realName, admin, grants, networks}})

	if m.CreateErr != nil {
		return nil, m.CreateErr
	}

	err := validateContactInfo(email, phone, realName)
	if err != nil {
		return nil, err
	}

	if _, exists := m.accounts[username]; exists {
		return nil, ErrDuplicateUsername
	}

	now := time.Now()
	acct := &Account{
		Username:     username,
		PasswordHash: password,
		Email:        email,
		Phone:        phone,
		RealName:     realName,
		Admin:        admin,
		Grants:       grants,
		Networks:     networks,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.accounts[username] = acct

	out := *acct
	return &out, nil
}

func (m *MockManager) Get(username string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Get", Args: []any{username}})

	if m.GetErr != nil {
		return nil, m.GetErr
	}

	acct, ok := m.accounts[username]
	if !ok {
		return nil, ErrNotFound
	}

	out := *acct
	return &out, nil
}

func (m *MockManager) Update(username string, fields UpdateFields) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Update", Args: []any{username, fields}})

	if m.UpdateErr != nil {
		return nil, m.UpdateErr
	}

	err := validateUpdateFields(fields)
	if err != nil {
		return nil, err
	}

	acct, ok := m.accounts[username]
	if !ok {
		return nil, ErrNotFound
	}

	// Resolve the resulting grant/admin/scope state and validate it before
	// mutating, mirroring the SQLite manager. acct is a live pointer into
	// the map, so applying first and failing after would corrupt stored state.
	//
	// The two implementations have to agree here: a mock that permitted a state
	// the real store refuses would let a handler test pass against a row
	// production can never hold.
	grants := acct.Grants
	if fields.Grants != nil {
		grants = normalizeGrants(*fields.Grants)
	}
	admin := acct.Admin
	if fields.Admin != nil {
		admin = *fields.Admin
	}
	networks := acct.Networks
	if fields.Networks != nil {
		networks = normalizeNetworkScope(*fields.Networks)
	}
	if len(grants) > 0 && admin {
		return nil, ErrGrantsAdmin
	}
	if len(grants) > 0 && len(networks) == 0 {
		return nil, ErrGrantsNoNetworks
	}

	if fields.Password != nil {
		acct.PasswordHash = *fields.Password
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
	acct.Grants = grants
	acct.Networks = networks
	acct.UpdatedAt = time.Now()

	out := *acct
	return &out, nil
}

func (m *MockManager) Disable(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Disable", Args: []any{username}})

	if m.DisableErr != nil {
		return m.DisableErr
	}

	acct, ok := m.accounts[username]
	if !ok {
		return ErrNotFound
	}

	acct.Disabled = true
	return nil
}

func (m *MockManager) Enable(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Enable", Args: []any{username}})

	if m.EnableErr != nil {
		return m.EnableErr
	}

	acct, ok := m.accounts[username]
	if !ok {
		return ErrNotFound
	}

	acct.Disabled = false
	return nil
}

func (m *MockManager) List() ([]Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "List", Args: nil})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	out := make([]Account, 0, len(m.accounts))
	for _, acct := range m.accounts {
		out = append(out, *acct)
	}
	return out, nil
}

func (m *MockManager) Authenticate(username, password string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Authenticate", Args: []any{username, password}})

	if m.AuthenticateErr != nil {
		return nil, m.AuthenticateErr
	}

	acct, ok := m.accounts[username]
	if !ok {
		return nil, ErrInvalidCredentials
	}

	if acct.Disabled {
		return nil, ErrAccountDisabled
	}

	if acct.PasswordHash != password {
		return nil, ErrInvalidCredentials
	}

	out := *acct
	return &out, nil
}

// --- MockSessionManager ---

type MockSessionManager struct {
	mu         sync.Mutex
	sessions   map[string]*Session
	accountMgr Manager
	Calls      []MockCall

	CreateErr                 error
	ValidateErr               error
	RevokeErr                 error
	RevokeAllErr              error
	CleanupErr                error
	ListErr                   error
	GetUsernameErr            error
	HasActiveAdminSessionsErr error
}

func InitMockSessionManager(mgr Manager) *MockSessionManager {
	return &MockSessionManager{
		sessions:   map[string]*Session{},
		accountMgr: mgr,
	}
}

func (m *MockSessionManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockSessionManager) Create(_ context.Context, username string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Create", Args: []any{username}})

	if m.CreateErr != nil {
		return "", m.CreateErr
	}

	now := time.Now()
	id := uuid.New().String()
	sess := &Session{
		ID:        id,
		Username:  username,
		CreatedAt: now,
		LastUsed:  now,
	}
	m.sessions[id] = sess

	return id, nil
}

func (m *MockSessionManager) Validate(_ context.Context, token string) (*Session, *Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Validate", Args: []any{token}})

	if m.ValidateErr != nil {
		return nil, nil, m.ValidateErr
	}

	sess, ok := m.sessions[token]
	if !ok {
		return nil, nil, ErrSessionNotFound
	}

	if time.Since(sess.LastUsed) > SessionMaxAge {
		delete(m.sessions, token)
		return nil, nil, ErrSessionExpired
	}

	sess.LastUsed = time.Now()

	acct, err := m.accountMgr.Get(sess.Username)
	if err != nil {
		return nil, nil, err
	}

	// Mirrors SQLiteSessionManager.Validate: a disabled account's token is
	// dead on arrival, whatever rows survive.
	if acct.Disabled {
		return nil, nil, ErrAccountDisabled
	}

	outSess := *sess
	return &outSess, acct, nil
}

func (m *MockSessionManager) Revoke(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Revoke", Args: []any{sessionID}})

	if m.RevokeErr != nil {
		return m.RevokeErr
	}

	if _, ok := m.sessions[sessionID]; !ok {
		return ErrSessionNotFound
	}

	delete(m.sessions, sessionID)
	return nil
}

func (m *MockSessionManager) RevokeAllForUser(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RevokeAllForUser", Args: []any{username}})

	if m.RevokeAllErr != nil {
		return m.RevokeAllErr
	}

	for id, sess := range m.sessions {
		if sess.Username == username {
			delete(m.sessions, id)
		}
	}
	return nil
}

func (m *MockSessionManager) Cleanup(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Cleanup", Args: nil})

	if m.CleanupErr != nil {
		return m.CleanupErr
	}

	for id, sess := range m.sessions {
		if time.Since(sess.LastUsed) > SessionMaxAge {
			delete(m.sessions, id)
		}
	}
	return nil
}

func (m *MockSessionManager) List(_ context.Context, username string) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "List", Args: []any{username}})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	var out []Session
	for _, sess := range m.sessions {
		if sess.Username == username {
			out = append(out, *sess)
		}
	}
	return out, nil
}

func (m *MockSessionManager) GetUsername(_ context.Context, sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetUsername", Args: []any{sessionID}})

	if m.GetUsernameErr != nil {
		return "", m.GetUsernameErr
	}

	sess, ok := m.sessions[sessionID]
	if !ok {
		return "", ErrSessionNotFound
	}
	return sess.Username, nil
}

func (m *MockSessionManager) HasActiveAdminSessions(_ context.Context, adminUsernames []string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "HasActiveAdminSessions", Args: []any{adminUsernames}})

	if m.HasActiveAdminSessionsErr != nil {
		return false, m.HasActiveAdminSessionsErr
	}

	if len(adminUsernames) == 0 {
		return false, nil
	}

	usernameSet := make(map[string]bool, len(adminUsernames))
	for _, u := range adminUsernames {
		usernameSet[u] = true
	}

	for _, sess := range m.sessions {
		if usernameSet[sess.Username] && time.Since(sess.LastUsed) <= SessionMaxAge {
			return true, nil
		}
	}

	return false, nil
}

func (m *MockSessionManager) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := m.Cleanup(ctx)
				if err != nil {
					slog.Error("session cleanup error", "error", err)
				}
			}
		}
	}()
}
