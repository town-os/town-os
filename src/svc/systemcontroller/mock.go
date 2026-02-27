package systemcontroller

import (
	"maps"
	"sync"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// MockClient is an in-memory implementation of [Client] for use in tests.
// It records every method call in the Calls slice and returns data from its
// exported fields. Injecting a non-nil error field (e.g. CreateErr) causes the
// corresponding method to return that error instead of proceeding normally.
type MockClient struct {
	mu                   sync.Mutex
	Filesystems          map[string]storage.Filesystem
	Repositories         []RepositoryInfo
	Packages             []string
	Questions            map[string]map[string]packages.Question
	Installed            []string
	StoredResponses      map[string]packages.Responses
	DisabledPackages     map[string]bool
	Units                []systemd.UnitStatus
	JournalEntries       []systemd.JournalEntry
	Accounts             map[string]*account.Account
	Sessions             map[string]*account.Session
	Calls                []MockCall
	CreateErr            error
	ModifyErr            error
	RemoveErr            error
	ListErr              error
	AddRepoErr           error
	RemRepoErr           error
	ListRepoErr          error
	ListPkgErr           error
	ListPkgVersionsErr   error
	QuestionsErr         error
	QuestionsIdentityErr error
	ListChildrenErr      error
	ChildrenMap          map[string][]string
	InstallPreviewErr    error
	InstallPreviewResult *InstallPreview
	InstallPkgErr        error
	UninstallPkgErr      error
	DisablePkgErr        error
	EnablePkgErr         error
	ListInstalledErr     error
	GetResponsesErr      error
	ListUnitsErr         error
	SetStatusErr         error
	LogReplayErr         error
	PingErr              error
	PingResponse         *PingResponse
	UpgradesList         []PackageUpgrade
	ListUpgradesErr      error
	DismissUpgradesErr   error
	CreateAcctErr        error
	GetAcctErr           error
	UpdateAcctErr        error
	DisableAcctErr       error
	EnableAcctErr        error
	ListAcctErr          error
	AuthenticateErr      error
	RevokeSessionErr     error
	ListSessionsErr      error
	SessionUsernameErr   error
	AuthToken            string
	AuditEntries         []account.AuditEntry
	ListAuditErr         error
	Settings             map[string]string
	UploadArchiveErr     error
	UploadArchiveResult  *ArchiveUploadResponse
	DownloadArchiveErr   error
	DownloadArchiveData  []byte
	RebuildGitErr        error
	Pages                map[string]*account.PageSite
	CreatePageErr        error
	UpdatePageErr        error
	RemovePageErr        error
	ListPagesErr         error
	RebuildPageErr       error
}

// MockCall records a single method invocation on [MockClient], including
// the method name and the arguments it was called with.
type MockCall struct {
	Method string
	Args   []any
}

// InitMockClient creates a [MockClient] with empty collections and default
// settings, ready for use in tests.
func InitMockClient() *MockClient {
	settings := make(map[string]string, len(account.DefaultSettings))
	maps.Copy(settings, account.DefaultSettings)

	return &MockClient{
		Filesystems:      map[string]storage.Filesystem{},
		StoredResponses:  map[string]packages.Responses{},
		DisabledPackages: map[string]bool{},
		Accounts:         map[string]*account.Account{},
		Sessions:         map[string]*account.Session{},
		Settings:         settings,
		Pages:            map[string]*account.PageSite{},
	}
}

// GetCalls returns a snapshot of all recorded method calls.
func (m *MockClient) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}
