package systemcontroller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

type MockClient struct {
	mu              sync.Mutex
	Filesystems     map[string]storage.Filesystem
	Repositories    []RepositoryInfo
	Packages        []string
	Questions       map[string]map[string]packages.Question
	Installed       []string
	StoredResponses map[string]packages.Responses
	Units           []systemd.UnitStatus
	JournalEntries  []systemd.JournalEntry
	Accounts        map[string]*account.Account
	Sessions        map[string]*account.Session
	Calls           []MockCall
	CreateErr       error
	ModifyErr       error
	RemoveErr       error
	ListErr         error
	AddRepoErr      error
	RemRepoErr      error
	ListRepoErr     error
	ListPkgErr      error
	QuestionsErr         error
	QuestionsIdentityErr error
	InstallPkgErr        error
	UninstallPkgErr error
	ListInstalledErr error
	GetResponsesErr error
	ListUnitsErr    error
	SetStatusErr    error
	LogReplayErr    error
	PingErr         error
	PingResponse    *PingResponse
	CreateAcctErr      error
	GetAcctErr         error
	UpdateAcctErr      error
	DisableAcctErr     error
	EnableAcctErr      error
	ListAcctErr        error
	AuthenticateErr    error
	RevokeSessionErr   error
	ListSessionsErr    error
	SessionUsernameErr error
	AuthToken          string
	AuditEntries       []account.AuditEntry
	ListAuditErr       error
}

type MockCall struct {
	Method string
	Args   []any
}

func InitMockClient() *MockClient {
	return &MockClient{
		Filesystems:     map[string]storage.Filesystem{},
		StoredResponses: map[string]packages.Responses{},
		Accounts:        map[string]*account.Account{},
		Sessions:        map[string]*account.Session{},
	}
}

func (m *MockClient) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

// --- Storage ---

func (m *MockClient) CreateFilesystem(_ context.Context, fs storage.Filesystem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "CreateFilesystem", Args: []any{fs}})

	if m.CreateErr != nil {
		return m.CreateErr
	}

	m.Filesystems[fs.Name] = fs
	return nil
}

func (m *MockClient) ModifyFilesystem(_ context.Context, name string, fs storage.Filesystem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ModifyFilesystem", Args: []any{name, fs}})

	if m.ModifyErr != nil {
		return m.ModifyErr
	}

	if _, ok := m.Filesystems[name]; !ok {
		return fmt.Errorf("filesystem %s not found", name)
	}

	if name != fs.Name {
		delete(m.Filesystems, name)
	}

	m.Filesystems[fs.Name] = fs
	return nil
}

func (m *MockClient) RemoveFilesystem(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveFilesystem", Args: []any{name}})

	if m.RemoveErr != nil {
		return m.RemoveErr
	}

	delete(m.Filesystems, name)
	return nil
}

func (m *MockClient) ListFilesystems(_ context.Context, prefix string) ([]storage.Filesystem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListFilesystems", Args: []any{prefix}})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	var out []storage.Filesystem
	for _, fs := range m.Filesystems {
		if prefix == "" || len(fs.Name) >= len(prefix) && fs.Name[:len(prefix)] == prefix {
			out = append(out, fs)
		}
	}

	return out, nil
}

// --- Repository ---

func (m *MockClient) AddRepository(_ context.Context, name, rawURL, username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddRepository", Args: []any{name, rawURL, username, password}})

	if m.AddRepoErr != nil {
		return m.AddRepoErr
	}

	for _, r := range m.Repositories {
		if r.URL == rawURL {
			return fmt.Errorf("repository %s already exists", rawURL)
		}
	}

	if name == "" {
		name = rawURL
	}
	m.Repositories = append(m.Repositories, RepositoryInfo{Name: name, URL: rawURL})
	return nil
}

func (m *MockClient) RemoveRepository(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveRepository", Args: []any{name}})

	if m.RemRepoErr != nil {
		return m.RemRepoErr
	}

	for i, r := range m.Repositories {
		if r.Name == name {
			m.Repositories = append(m.Repositories[:i], m.Repositories[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("repository %s not found", name)
}

func (m *MockClient) RefreshRepositories(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RefreshRepositories", Args: nil})
	return nil, nil
}

func (m *MockClient) ListRepositories(_ context.Context, params ListParams) (*PageResult[RepositoryInfo], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListRepositories", Args: []any{params}})

	if m.ListRepoErr != nil {
		return nil, m.ListRepoErr
	}

	out := make([]RepositoryInfo, len(m.Repositories))
	copy(out, m.Repositories)
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}

// --- Packages ---

func (m *MockClient) ListPackages(_ context.Context, params ListParams) (*PageResult[string], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackages", Args: []any{params}})

	if m.ListPkgErr != nil {
		return nil, m.ListPkgErr
	}

	out := make([]string, len(m.Packages))
	copy(out, m.Packages)
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) GetPackageQuestions(_ context.Context, name string) (map[string]packages.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetPackageQuestions", Args: []any{name}})

	if m.QuestionsErr != nil {
		return nil, m.QuestionsErr
	}

	questions, ok := m.Questions[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	out := make(map[string]packages.Question, len(questions))
	for k, v := range questions {
		out[k] = v
	}
	return out, nil
}

func (m *MockClient) GetPackageQuestionsByIdentity(_ context.Context, name, version string) (map[string]packages.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetPackageQuestionsByIdentity", Args: []any{name, version}})

	if m.QuestionsIdentityErr != nil {
		return nil, m.QuestionsIdentityErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	questions, ok := m.Questions[key]
	if !ok {
		return nil, fmt.Errorf("package %s not found", key)
	}

	out := make(map[string]packages.Question, len(questions))
	for k, v := range questions {
		out[k] = v
	}
	return out, nil
}

// --- Install ---

func (m *MockClient) InstallPackage(_ context.Context, name, version string, responses packages.Responses) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "InstallPackage", Args: []any{name, version, responses}})

	if m.InstallPkgErr != nil {
		return m.InstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	m.Installed = append(m.Installed, key)
	m.StoredResponses[key] = responses
	return nil
}

func (m *MockClient) UninstallPackage(_ context.Context, name, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UninstallPackage", Args: []any{name, version}})

	if m.UninstallPkgErr != nil {
		return m.UninstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	for i, p := range m.Installed {
		if p == key {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			delete(m.StoredResponses, key)
			return nil
		}
	}

	return fmt.Errorf("%s: not installed", key)
}

func (m *MockClient) ListInstalled(_ context.Context, params ListParams) (*PageResult[string], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListInstalled", Args: []any{params}})

	if m.ListInstalledErr != nil {
		return nil, m.ListInstalledErr
	}

	out := make([]string, len(m.Installed))
	copy(out, m.Installed)
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) GetResponses(_ context.Context, name, version string) (packages.Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetResponses", Args: []any{name, version}})

	if m.GetResponsesErr != nil {
		return nil, m.GetResponsesErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s: not installed", key)
	}

	out := make(packages.Responses, len(resp))
	for k, v := range resp {
		out[k] = v
	}
	return out, nil
}

// --- Systemd ---

func (m *MockClient) ListUnits(_ context.Context, params ListParams) (*PageResult[systemd.UnitStatus], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListUnits", Args: []any{params}})

	if m.ListUnitsErr != nil {
		return nil, m.ListUnitsErr
	}

	out := make([]systemd.UnitStatus, len(m.Units))
	copy(out, m.Units)
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) SetUnitStatus(_ context.Context, name string, action systemd.StatusAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetUnitStatus", Args: []any{name, action}})

	if m.SetStatusErr != nil {
		return m.SetStatusErr
	}

	switch action {
	case systemd.Start, systemd.Stop, systemd.Restart, systemd.Enable, systemd.Disable:
		return nil
	default:
		return fmt.Errorf("%q: %w", action, systemd.ErrInvalidAction)
	}
}

func (m *MockClient) LogReplay(_ context.Context, name string) (<-chan systemd.JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "LogReplay", Args: []any{name}})

	if m.LogReplayErr != nil {
		return nil, m.LogReplayErr
	}

	entries := make([]systemd.JournalEntry, len(m.JournalEntries))
	copy(entries, m.JournalEntries)

	ch := make(chan systemd.JournalEntry)
	go func() {
		defer close(ch)
		for _, e := range entries {
			ch <- e
		}
	}()

	return ch, nil
}

func (m *MockClient) LogTail(_ context.Context, p systemd.LogTailParams) (systemd.LogTailResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "LogTail", Args: []any{p}})

	if m.LogReplayErr != nil {
		return systemd.LogTailResult{}, m.LogReplayErr
	}

	entries := make([]systemd.JournalEntry, len(m.JournalEntries))
	copy(entries, m.JournalEntries)

	endIdx := len(entries)
	if p.BeforeCursor != "" {
		for i, e := range entries {
			if e.Cursor == p.BeforeCursor {
				endIdx = i
				break
			}
		}
	}

	startIdx := endIdx - p.Lines
	if startIdx < 0 {
		startIdx = 0
	}

	page := entries[startIdx:endIdx]

	var cursor string
	if len(page) > 0 {
		cursor = page[0].Cursor
	}

	return systemd.LogTailResult{Entries: page, Cursor: cursor}, nil
}

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

func (m *MockClient) ListAccounts(_ context.Context, _, _ string) ([]account.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListAccounts", Args: nil})

	if m.ListAcctErr != nil {
		return nil, m.ListAcctErr
	}

	var out []account.Account
	for _, acct := range m.Accounts {
		out = append(out, *acct)
	}
	return out, nil
}

func (m *MockClient) Authenticate(_ context.Context, username, password string) (*AuthenticateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Authenticate", Args: []any{username, password}})

	if m.AuthenticateErr != nil {
		return nil, m.AuthenticateErr
	}

	acct, ok := m.Accounts[username]
	if !ok {
		return nil, fmt.Errorf("invalid credentials")
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
	return "", fmt.Errorf("no sessions")
}

// --- Audit ---

func (m *MockClient) ListAuditLog(_ context.Context, opts account.AuditListOptions, token string) (*account.AuditPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListAuditLog", Args: []any{opts, token}})

	if m.ListAuditErr != nil {
		return nil, m.ListAuditErr
	}

	entries := make([]account.AuditEntry, len(m.AuditEntries))
	copy(entries, m.AuditEntries)

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	totalPages := (len(entries) + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	return &account.AuditPage{Entries: entries, TotalPages: totalPages}, nil
}

// --- Status ---

func (m *MockClient) Ping(_ context.Context) (*PingResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Ping", Args: nil})

	if m.PingErr != nil {
		return nil, m.PingErr
	}

	if m.PingResponse != nil {
		return m.PingResponse, nil
	}

	return &PingResponse{Status: "ok"}, nil
}
