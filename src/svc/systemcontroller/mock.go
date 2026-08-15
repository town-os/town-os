package systemcontroller

import (
	"maps"
	"sync"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// MockClient is an in-memory implementation of [Client] for use in tests.
// It records every method call in the Calls slice and returns data from its
// exported fields. Injecting a non-nil error field (e.g. CreateErr) causes the
// corresponding method to return that error instead of proceeding normally.
type MockClient struct {
	mu                        sync.Mutex
	Filesystems               map[string]storage.Filesystem
	Repositories              []RepositoryInfo
	Packages                  []string
	Questions                 map[string]map[string]packages.Question
	Installed                 []string
	StoredResponses           map[string]packages.Responses
	DisabledPackages          map[string]bool
	Units                     []systemd.UnitStatus
	JournalEntries            []systemd.JournalEntry
	Accounts                  map[string]*account.Account
	Sessions                  map[string]*account.Session
	Calls                     []MockCall
	CreateErr                 error
	ModifyErr                 error
	RemoveErr                 error
	ListErr                   error
	AddRepoErr                error
	RemRepoErr                error
	ListRepoErr               error
	FeaturedGroups            []FeaturedRepoGroup
	ListFeaturedErr           error
	ListPkgErr                error
	ListPkgVersionsErr        error
	QuestionsErr              error
	QuestionsIdentityErr      error
	ListChildrenErr           error
	ChildrenMap               map[string][]string
	InstallPreviewErr         error
	InstallPreviewResult      *InstallPreview
	InstallPkgErr             error
	UninstallPkgErr           error
	DisablePkgErr             error
	EnablePkgErr              error
	ListInstalledErr          error
	GetResponsesErr           error
	ListUnitsErr              error
	SetStatusErr              error
	LogReplayErr              error
	PingErr                   error
	PingResponse              *PingResponse
	GetMetricsErr             error
	MetricsBody               string
	UpgradesList              []PackageUpgrade
	ListUpgradesErr           error
	DismissUpgradesErr        error
	CreateAcctErr             error
	GetAcctErr                error
	UpdateAcctErr             error
	DisableAcctErr            error
	EnableAcctErr             error
	ListAcctErr               error
	AuthenticateErr           error
	RevokeSessionErr          error
	ListSessionsErr           error
	SessionUsernameErr        error
	AuthToken                 string
	AuditEntries              []account.AuditEntry
	ListAuditErr              error
	Settings                  map[string]string
	UploadArchiveErr          error
	UploadArchiveResult       *ArchiveUploadResponse
	DownloadArchiveErr        error
	DownloadArchiveData       []byte
	RebuildGitErr             error
	Pages                     map[string]*account.PageSite
	CreatePageErr             error
	UpdatePageErr             error
	RemovePageErr             error
	ListPagesErr              error
	RebuildPageErr            error
	UploadPageArchiveErr      error
	ListLocalesErr            error
	MonitoringStatusResp      *monitoring.MonitoringStatus
	MonitoringStatusErr       error
	SystemServices            []SystemServiceEntry
	ListSystemServicesErr     error
	SetSystemServiceStatusErr error

	// gfeh partitions, keyed by volume name (with the gfeh/ prefix), so the
	// mock reflects the same namespace the real handlers report.
	GfehPartitions         map[string]storage.Filesystem
	CreateGfehPartitionErr error
	ModifyGfehPartitionErr error
	RemoveGfehPartitionErr error
	ListGfehPartitionsErr  error
	VMImages               []VMImageInfo
	ListVMImagesErr        error
	UploadVMImageErr       error
	UploadVMImageResult    *VMImageInfo
	DeleteVMImageErr       error

	DNSStatusResp      *DNSStatusResponse
	DNSStatusErr       error
	DNSRecords         []*upstream.DnsRecord
	ListDNSRecordsErr  error
	AddDNSRecordErr    error
	RemoveDNSRecordErr error
	DNSRemoveCount     uint32
	DNSTLD             string
	GetDNSTLDErr       error
	SetDNSTLDErr       error
	SetupDNSErr        error

	DnsblConfig             *BlocklistConfigResponse
	GetDnsblConfigErr       error
	SetDnsblConfigErr       error
	LocalBlocklistEntries   []LocalBlocklistEntryDTO
	ListLocalBlocklistErr   error
	AddLocalBlocklistErr    error
	RemoveLocalBlocklistErr error

	DnsblAllowlistEntries   []DnsblAllowlistEntryDTO
	ListDnsblAllowlistErr   error
	AddDnsblAllowlistErr    error
	RemoveDnsblAllowlistErr error

	DNSServices        []DNSServiceEntry
	ListDNSServicesErr error
	SetDNSServiceErr   error
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
