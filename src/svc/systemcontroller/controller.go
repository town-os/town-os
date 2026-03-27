package systemcontroller

import (
	"context"
	"net/http"
	"sync"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
	"github.com/labstack/echo/v5"
)

type systemControllerBackend interface {
	GetStorage() storage.Storage
	GetRepositoryRoot() *packages.RepositoryRoot
	GetInstaller() packages.Installer
	GetSystemdManager() systemd.Manager
	GetAccountManager() account.Manager
	GetSessionManager() account.SessionManager
	GetAuditManager() account.AuditManager
	GetSettingsManager() account.SettingsManager
	GetPagesStore() *PagesStore
	GetGitClient() git.Client
	GetAllowedHosts() []string
	GetDefaultRepoCredentials() (string, string)
	GetBtrfsBasePath() string
	GetNetworkControllerImage() string
	GetNetworkStatePath() string
	GetExternalIP() string
	GetInternalIP() string
	GetGitCloner() packages.GitCloner
	GetPagesManager() account.PagesManager
	GetMonitoring() *monitoring.Manager
	GetRolodex() *rolodex.Manager
	GetRolodexClient() rolodex.Client
	GetUI() *ui.Manager
	GetImageExtractFunc() func(ctx context.Context, image, directory, targetPath string) error
}

type SystemController interface {
	systemControllerBackend
	Run() error
	Client() (*SystemdClient, error)
}

type SystemControllerHandlers struct {
	Controller systemControllerBackend

	// ctx is the server-scoped context used by background goroutines that
	// must outlive HTTP requests but respect graceful shutdown.
	ctx context.Context //nolint:containedctx // intentional: background goroutines need server lifetime

	// packageMu serializes mutating operations (install, uninstall, purge,
	// enable/disable, rebuild-git) on the same package so that concurrent
	// requests cannot interleave and leave the service in an inconsistent
	// state (e.g. old container still stopping while a new one tries to
	// bind the same ports).
	packageMu sync.Map // "repo/name" → *sync.Mutex
}

// lockPackage acquires a per-package mutex and returns an unlock function.
// All mutating package operations must hold this lock for the duration of
// the handler to prevent races between e.g. uninstall+purge and install.
func (s *SystemControllerHandlers) lockPackage(repo, name string) func() {
	key := repo + "/" + name
	actual, _ := s.packageMu.LoadOrStore(key, &sync.Mutex{})
	mu, ok := actual.(*sync.Mutex)
	if !ok {
		// Should never happen: we only store *sync.Mutex values.
		mu = &sync.Mutex{}
		s.packageMu.Store(key, mu)
	}
	mu.Lock()
	return mu.Unlock
}

func getHandler(ctx context.Context, sc systemControllerBackend) *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: sc, ctx: ctx}
}

// --- Routes ---

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	// Public
	e.Add("GET", "/status/ping", s.ping)
	e.Add("POST", "/account/authenticate", s.authenticateAccount)

	// Self-authenticated (handlers do own token validation)
	e.Add("GET", "/account/sessions", s.listSessions)
	e.Add("GET", "/account/me", s.sessionUsername)
	e.Add("POST", "/account/session/revoke", s.revokeSession)

	// Authenticated (requireAuth)
	e.Add("POST", "/storage/create", s.createFilesystem, s.requireAuth)
	e.Add("POST", "/storage/modify", s.modifyFilesystem, s.requireAuth)
	e.Add("POST", "/storage/remove", s.removeFilesystem, s.requireAuth)
	e.Add("POST", "/storage", s.listFilesystems, s.requireAuth)
	e.Add("POST", "/storage/package-volumes", s.listPackageVolumes, s.requireAuth)
	e.Add("POST", "/storage/remove-package-volume", s.removePackageVolume, s.requireAdmin)

	e.Add("POST", "/repository/add", s.addRepository, s.requireAuth)
	e.Add("POST", "/repository/remove", s.removeRepository, s.requireAuth)
	e.Add("POST", "/repository/move", s.moveRepository, s.requireAdmin)
	e.Add("POST", "/repository/refresh", s.refreshRepositories, s.requireAuth)
	e.Add("GET", "/repository", s.listRepositories, s.requireAuth)

	e.Add("GET", "/packages", s.listPackages, s.requireAuth)
	e.Add("GET", "/packages/by-repo", s.listPackagesByRepo, s.requireAuth)
	e.Add("GET", "/packages/featured", s.listFeaturedPackages, s.requireAuth)
	e.Add("POST", "/packages/versions", s.listPackageVersions, s.requireAuth)
	e.Add("GET", "/packages/installed", s.listInstalled, s.requireAuth)
	e.Add("POST", "/packages/installed/info", s.getInstalledInfo, s.requireAuth)
	e.Add("POST", "/packages/responses", s.getResponses, s.requireAuth)
	e.Add("POST", "/packages/last-responses", s.getLastResponses, s.requireAuth)
	e.Add("POST", "/packages/manifest", s.packageManifest, s.requireAuth)
	e.Add("POST", "/packages/clear-last-responses", s.clearLastResponses, s.requireAdmin)

	e.Add("GET", "/systemd/units", s.listUnits, s.localhostOrAuth)
	e.Add("GET", "/systemd/logs", s.logReplay, s.localhostOrAuth)
	e.Add("GET", "/systemd/logs/tail", s.logTail, s.localhostOrAuth)

	e.Add("POST", "/account/create", s.createAccount)
	e.Add("POST", "/account", s.getAccount, s.requireAuth)
	e.Add("POST", "/account/update", s.updateAccount, s.requireAuth)
	e.Add("GET", "/account", s.listAccounts, s.requireAuth)

	// Admin (requireAdmin, which implies auth)
	e.Add("POST", "/packages/questions", s.getPackageQuestions, s.requireAdmin)
	e.Add("POST", "/packages/questions/identity", s.getPackageQuestionsByIdentity, s.requireAdmin)
	e.Add("POST", "/packages/children", s.listChildren, s.requireAuth)
	e.Add("POST", "/packages/install-preview", s.installPreview, s.requireAuth)
	e.Add("POST", "/packages/install", s.installPackage, s.requireAdmin)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage, s.requireAdmin)
	e.Add("POST", "/packages/purge-volumes", s.purgeVolumes, s.requireAdmin)
	e.Add("POST", "/packages/uninstalled-volumes", s.listUninstalledVolumes, s.requireAuth)
	e.Add("POST", "/packages/purge-uninstalled-volumes", s.purgeUninstalledVolumes, s.requireAdmin)
	e.Add("GET", "/packages/upgrades", s.listUpgrades, s.requireAuth)
	e.Add("POST", "/packages/upgrades/dismiss", s.dismissUpgrades, s.requireAdmin)
	e.Add("POST", "/packages/disable", s.disablePackage, s.requireAdmin)
	e.Add("POST", "/packages/enable", s.enablePackage, s.requireAdmin)
	e.Add("POST", "/systemd/status", s.setUnitStatus, s.requireAdmin)
	e.Add("POST", "/account/disable", s.disableAccount, s.requireAdmin)
	e.Add("POST", "/account/enable", s.enableAccount, s.requireAdmin)
	e.Add("POST", "/audit/log", s.listAuditLog, s.requireAdmin)
	e.Add("GET", "/settings", s.getSettings, s.requireAdmin)
	e.Add("POST", "/settings/get", s.getSetting, s.requireAdmin)
	e.Add("POST", "/settings/set", s.setSetting, s.requireAdmin)
	e.Add("POST", "/storage/upload-archive", s.uploadArchive, s.requireAdmin)
	e.Add("POST", "/storage/download-archive", s.downloadArchive, s.requireAdmin)
	e.Add("POST", "/packages/rebuild-git", s.rebuildGit, s.requireAdmin)
	e.Add("POST", "/pages/create", s.createPage, s.requireAdmin)
	e.Add("POST", "/pages/update", s.updatePage, s.requireAdmin)
	e.Add("POST", "/pages/remove", s.removePage, s.requireAdmin)
	e.Add("POST", "/pages/rebuild", s.rebuildPage, s.requireAdmin)
	e.Add("POST", "/pages/upload", s.uploadPageArchive, s.requireAdmin)
	e.Add("GET", "/pages", s.listPages, s.requireAuth)

	e.Add("GET", "/vm-images", s.listVMImages, s.requireAuth)
	e.Add("POST", "/vm-images/upload", s.uploadVMImage, s.requireAdmin)
	e.Add("POST", "/vm-images/delete", s.deleteVMImage, s.requireAdmin)

	e.Add("GET", "/locales", s.listLocales, s.requireAuth)

	// Monitoring
	e.Add("GET", "/monitoring/status", s.monitoringStatus, s.requireAuth)
	e.Add("GET", "/monitoring/grafana/*", s.grafanaProxy)

	// System Services
	e.Add("GET", "/system-services", s.listSystemServices, s.localhostOrAuth)
	e.Add("POST", "/system-services/status", s.setSystemServiceStatus, s.requireAdmin)
	e.Add("POST", "/system-services/refresh", s.refreshSystemServices, s.requireAdmin)

	// DNS
	e.Add("GET", "/dns/status", s.dnsStatus, s.requireAuth)
	e.Add("GET", "/dns/records", s.listDNSRecords, s.requireAuth)
	e.Add("POST", "/dns/records/add", s.addDNSRecord, s.requireAdmin)
	e.Add("POST", "/dns/records/remove", s.removeDNSRecord, s.requireAdmin)
	e.Add("GET", "/dns/tld", s.getDNSTLD, s.requireAuth)
	e.Add("POST", "/dns/tld", s.setDNSTLD, s.requireAdmin)
	e.Add("POST", "/dns/setup", s.setupDNS, s.requireAdmin)
}

// --- Server infrastructure ---

type ServerConfig struct {
	Storage                  storage.Storage
	RepositoryRoot           *packages.RepositoryRoot
	Installer                packages.Installer
	Systemd                  systemd.Manager
	AccountMgr               account.Manager
	SessionMgr               account.SessionManager
	AuditMgr                 account.AuditManager
	SettingsMgr              account.SettingsManager
	Pages                    *PagesStore
	Git                      git.Client
	AllowedHosts             []string
	DefaultRepoUser          string
	DefaultRepoPass          string
	BtrfsBasePath          string
	NetworkControllerImage string
	NetworkStatePath       string
	PagesMgr                 account.PagesManager
	GitCloner                packages.GitCloner
	Monitoring               *monitoring.Manager
	Rolodex                  *rolodex.Manager
	RolodexClient            rolodex.Client
	UI                       *ui.Manager
	ImageExtractFunc         func(ctx context.Context, image, directory, targetPath string) error
}

func withContext(parent context.Context, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-parent.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
		handler.ServeHTTP(w, r.WithContext(ctx)) //nolint:contextcheck // parent is monitored via goroutine above
	})
}
