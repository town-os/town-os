package systemcontroller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

type MockClient struct {
	mu               sync.Mutex
	Filesystems      map[string]storage.Filesystem
	Repositories     []RepositoryInfo
	Packages         []string
	Questions        map[string]map[string]packages.Question
	Installed        []string
	StoredResponses  map[string]packages.Responses
	DisabledPackages map[string]bool
	Units            []systemd.UnitStatus
	JournalEntries   []systemd.JournalEntry
	Accounts         map[string]*account.Account
	Sessions         map[string]*account.Session
	Calls            []MockCall
	CreateErr        error
	ModifyErr        error
	RemoveErr        error
	ListErr          error
	AddRepoErr       error
	RemRepoErr       error
	ListRepoErr      error
	ListPkgErr           error
	ListPkgVersionsErr   error
	QuestionsErr         error
	QuestionsIdentityErr error
	InstallPreviewErr    error
	InstallPreviewResult *InstallPreview
	InstallPkgErr        error
	UninstallPkgErr error
	DisablePkgErr   error
	EnablePkgErr    error
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
	Settings             map[string]string
	UploadArchiveErr     error
	UploadArchiveResult  *ArchiveUploadResponse
	DownloadArchiveErr   error
	DownloadArchiveData  []byte
}

type MockCall struct {
	Method string
	Args   []any
}

func InitMockClient() *MockClient {
	settings := make(map[string]string)
	for k, v := range account.DefaultSettings {
		settings[k] = v
	}

	return &MockClient{
		Filesystems:      map[string]storage.Filesystem{},
		StoredResponses:  map[string]packages.Responses{},
		DisabledPackages: map[string]bool{},
		Accounts:         map[string]*account.Account{},
		Sessions:         map[string]*account.Session{},
		Settings:         settings,
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

func (m *MockClient) ListFilesystems(_ context.Context, prefix string, state string, params ListParams) (*PageResult[storage.Filesystem], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListFilesystems", Args: []any{prefix, state, params}})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	var out []storage.Filesystem
	for _, fs := range m.Filesystems {
		if prefix == "" || len(fs.Name) >= len(prefix) && fs.Name[:len(prefix)] == prefix {
			out = append(out, fs)
		}
	}

	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
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

func (m *MockClient) MoveRepository(_ context.Context, name string, position int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "MoveRepository", Args: []any{name, position}})
	return nil
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

func (m *MockClient) ListPackages(_ context.Context, params ListParams) (*PageResult[PackageListEntry], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackages", Args: []any{params}})

	if m.ListPkgErr != nil {
		return nil, m.ListPkgErr
	}

	entries := make([]PackageListEntry, 0, len(m.Packages))
	for _, pkg := range m.Packages {
		parts := strings.SplitN(pkg, "/", 2)
		var repo, rest string
		if len(parts) == 2 {
			repo = parts[0]
			rest = parts[1]
		} else {
			rest = parts[0]
		}
		nameVer := strings.SplitN(rest, "@", 2)
		name := nameVer[0]
		version := ""
		if len(nameVer) == 2 {
			version = nameVer[1]
		}

		isInstalled := false
		instVersion := ""
		key := fmt.Sprintf("%s/%s", repo, name)
		for _, inst := range m.Installed {
			instParts := strings.SplitN(inst, "/", 2)
			var instRepo, instRest string
			if len(instParts) == 2 {
				instRepo = instParts[0]
				instRest = instParts[1]
			} else {
				instRest = instParts[0]
			}
			instNameVer := strings.SplitN(instRest, "@", 2)
			instName := instNameVer[0]
			if fmt.Sprintf("%s/%s", instRepo, instName) == key {
				isInstalled = true
				if len(instNameVer) == 2 {
					instVersion = instNameVer[1]
				}
				break
			}
		}

		entries = append(entries, PackageListEntry{
			Repo:             repo,
			Name:             name,
			Version:          version,
			Installed:        isInstalled,
			InstalledVersion: instVersion,
		})
	}

	entries = filterSearch(entries, params.Search)
	result := paginate(entries, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) ListPackagesByRepo(_ context.Context, _ ListParams) ([]packages.RepoPackageGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackagesByRepo", Args: nil})

	return nil, nil
}

func (m *MockClient) ListPackageVersions(_ context.Context, name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackageVersions", Args: []any{name}})

	if m.ListPkgVersionsErr != nil {
		return nil, m.ListPkgVersionsErr
	}

	// Collect versions from Packages list matching name.
	seen := map[string]bool{}
	for _, pkg := range m.Packages {
		parts := strings.SplitN(pkg, "@", 2)
		if len(parts) == 2 && parts[0] == name {
			seen[parts[1]] = true
		}
	}

	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}

	return out, nil
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

func (m *MockClient) GetPackageQuestionsByIdentity(_ context.Context, repo, name, version string) (map[string]packages.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetPackageQuestionsByIdentity", Args: []any{repo, name, version}})

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

func (m *MockClient) InstallPreview(_ context.Context, repo, name, version string) (*InstallPreview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "InstallPreview", Args: []any{repo, name, version}})

	if m.InstallPreviewErr != nil {
		return nil, m.InstallPreviewErr
	}
	if m.InstallPreviewResult != nil {
		return m.InstallPreviewResult, nil
	}
	return &InstallPreview{
		Repo:          repo,
		Name:          name,
		Version:       version,
		Volumes:       []VolumePreview{},
		ExternalPorts: []PortPreview{},
		InternalPorts: []PortPreview{},
	}, nil
}

func (m *MockClient) InstallPackage(_ context.Context, name, version string, responses packages.Responses, reuseVolumes bool, importFromVersion string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "InstallPackage", Args: []any{name, version, responses, reuseVolumes, importFromVersion}})

	if m.InstallPkgErr != nil {
		return m.InstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	m.Installed = append(m.Installed, key)
	m.StoredResponses[key] = responses
	return nil
}

func (m *MockClient) UninstallPackage(_ context.Context, repo, name, version string, purgeVolumes bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UninstallPackage", Args: []any{repo, name, version, purgeVolumes}})

	if m.UninstallPkgErr != nil {
		return m.UninstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	for i, p := range m.Installed {
		if p == key {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			delete(m.StoredResponses, key)

			if purgeVolumes {
				prefix := fmt.Sprintf("installed/%s/%s/", repo, name)
				for fsName := range m.Filesystems {
					if len(fsName) >= len(prefix) && fsName[:len(prefix)] == prefix {
						delete(m.Filesystems, fsName)
					}
				}
				delete(m.Filesystems, fmt.Sprintf("installed/%s/%s", repo, name))
			}

			return nil
		}
	}

	return fmt.Errorf("%s: not installed", key)
}

func (m *MockClient) PurgeVolumes(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "PurgeVolumes", Args: []any{repo, name}})

	prefix := fmt.Sprintf("installed/%s/%s/", repo, name)
	for fsName := range m.Filesystems {
		if len(fsName) >= len(prefix) && fsName[:len(prefix)] == prefix {
			delete(m.Filesystems, fsName)
		}
	}
	delete(m.Filesystems, fmt.Sprintf("installed/%s/%s", repo, name))

	return nil
}

func (m *MockClient) ListUninstalledVolumes(_ context.Context, repo, name string) (*UninstalledVolumesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListUninstalledVolumes", Args: []any{repo, name}})

	return &UninstalledVolumesResponse{}, nil
}

func (m *MockClient) PurgeUninstalledVolumes(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "PurgeUninstalledVolumes", Args: []any{repo, name}})

	return nil
}

func (m *MockClient) DisablePackage(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "DisablePackage", Args: []any{repo, name}})

	if m.DisablePkgErr != nil {
		return m.DisablePkgErr
	}

	m.DisabledPackages[name] = true
	return nil
}

func (m *MockClient) EnablePackage(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "EnablePackage", Args: []any{repo, name}})

	if m.EnablePkgErr != nil {
		return m.EnablePkgErr
	}

	delete(m.DisabledPackages, name)
	return nil
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

func (m *MockClient) GetResponses(_ context.Context, repo, name, version string) (packages.Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetResponses", Args: []any{repo, name, version}})

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

func (m *MockClient) GetInstalledInfo(_ context.Context, repo, name, version string) (*InstalledInfoResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetInstalledInfo", Args: []any{repo, name, version}})

	key := fmt.Sprintf("%s@%s", name, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s: not installed", key)
	}

	return &InstalledInfoResponse{
		Responses: resp,
	}, nil
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
	case systemd.Start, systemd.Stop, systemd.Restart:
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

	return &account.AuditPage{Entries: entries, TotalPages: totalPages, TotalCount: len(entries)}, nil
}

// --- Settings ---

func (m *MockClient) GetSettings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetSettings", Args: nil})

	out := make(map[string]string)
	for k, v := range m.Settings {
		out[k] = v
	}
	return out, nil
}

func (m *MockClient) GetSetting(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetSetting", Args: []any{key}})

	v, ok := m.Settings[key]
	if !ok {
		return "", fmt.Errorf("setting %q not found", key)
	}
	return v, nil
}

func (m *MockClient) SetSetting(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetSetting", Args: []any{key, value}})

	if m.Settings == nil {
		m.Settings = make(map[string]string)
	}
	m.Settings[key] = value
	return nil
}

// --- Status ---

func (m *MockClient) UploadArchive(_ context.Context, subvolume string, archiveReader io.Reader, filename string) (*ArchiveUploadResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UploadArchive", Args: []any{subvolume, filename}})

	if m.UploadArchiveErr != nil {
		return nil, m.UploadArchiveErr
	}

	if m.UploadArchiveResult != nil {
		return m.UploadArchiveResult, nil
	}

	return &ArchiveUploadResponse{NeedsRestart: true, Message: "archive unpacked successfully"}, nil
}

func (m *MockClient) DownloadArchive(_ context.Context, subvolumes []string, stopService string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "DownloadArchive", Args: []any{subvolumes, stopService}})

	if m.DownloadArchiveErr != nil {
		return nil, m.DownloadArchiveErr
	}

	data := m.DownloadArchiveData
	if data == nil {
		data = []byte("mock-7z-data")
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

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
