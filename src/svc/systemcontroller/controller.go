package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
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
	GetNetworkControllerBinPath() string
	GetNetworkStatePath() string
	GetNetworkMode() string
	GetExternalIP() string
	GetInternalIP() string
	GetGitCloner() packages.GitCloner
	GetPagesManager() account.PagesManager
	GetMonitoring() *monitoring.Manager
}

type SystemController interface {
	systemControllerBackend
	Run() error
	Client() (*SystemdClient, error)
}

type SystemControllerHandlers struct {
	Controller systemControllerBackend
}

func getHandler(sc systemControllerBackend) *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: sc}
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
	e.Add("POST", "/packages/clear-last-responses", s.clearLastResponses, s.requireAdmin)

	e.Add("GET", "/systemd/units", s.listUnits, s.requireAuth)
	e.Add("GET", "/systemd/logs", s.logReplay, s.requireAuth)
	e.Add("GET", "/systemd/logs/tail", s.logTail, s.requireAuth)

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

	// System Services
	e.Add("GET", "/system-services", s.listSystemServices, s.requireAuth)
	e.Add("POST", "/system-services/status", s.setSystemServiceStatus, s.requireAdmin)
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
	BtrfsBasePath            string
	NetworkControllerBinPath string
	NetworkStatePath         string
	NetworkMode              string
	PagesMgr                 account.PagesManager
	GitCloner                packages.GitCloner
	Monitoring               *monitoring.Manager
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

type serverBase struct {
	ServerConfig

	externalIP atomic.Value // stores string
}

func (s *serverBase) GetStorage() storage.Storage                 { return s.Storage }
func (s *serverBase) GetRepositoryRoot() *packages.RepositoryRoot { return s.RepositoryRoot }
func (s *serverBase) GetInstaller() packages.Installer            { return s.Installer }
func (s *serverBase) GetSystemdManager() systemd.Manager          { return s.Systemd }
func (s *serverBase) GetAccountManager() account.Manager          { return s.AccountMgr }
func (s *serverBase) GetSessionManager() account.SessionManager   { return s.SessionMgr }
func (s *serverBase) GetAuditManager() account.AuditManager       { return s.AuditMgr }
func (s *serverBase) GetSettingsManager() account.SettingsManager  { return s.SettingsMgr }
func (s *serverBase) GetPagesStore() *PagesStore                  { return s.Pages }
func (s *serverBase) GetGitClient() git.Client                    { return s.Git }
func (s *serverBase) GetAllowedHosts() []string                   { return s.AllowedHosts }
func (s *serverBase) GetDefaultRepoCredentials() (string, string) {
	return s.DefaultRepoUser, s.DefaultRepoPass
}
func (s *serverBase) GetBtrfsBasePath() string            { return s.BtrfsBasePath }
func (s *serverBase) GetNetworkControllerBinPath() string { return s.NetworkControllerBinPath }
func (s *serverBase) GetNetworkStatePath() string         { return s.NetworkStatePath }
func (s *serverBase) GetNetworkMode() string              { return s.NetworkMode }
func (s *serverBase) GetGitCloner() packages.GitCloner {
	if s.GitCloner != nil {
		return s.GitCloner
	}
	return packages.DefaultGitCloner{}
}
func (s *serverBase) GetPagesManager() account.PagesManager { return s.PagesMgr }
func (s *serverBase) GetMonitoring() *monitoring.Manager     { return s.Monitoring }
func (s *serverBase) GetExternalIP() string {
	v := s.externalIP.Load()
	if v == nil {
		return ""
	}
	ip, _ := v.(string)
	return ip
}

func (s *serverBase) GetInternalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func (s *serverBase) fetchExternalIP(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/json", nil)
	if reqErr != nil {
		slog.Debug(fmt.Sprintf("fetchExternalIP: %v", reqErr))
		return
	}
	client := &http.Client{}
	resp, err := client.Do(req) //nolint:gosec // G704 -- URL is a constant
	if err != nil {
		slog.Debug(fmt.Sprintf("fetchExternalIP: %v", err))
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Debug(fmt.Sprintf("fetchExternalIP: close body: %v", err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("fetchExternalIP: status %d", resp.StatusCode)) //nolint:gosec // G706 -- status code is not tainted
		return
	}

	var result struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug(fmt.Sprintf("fetchExternalIP: decode: %v", err))
		return
	}

	if result.IP != "" {
		s.externalIP.Store(result.IP)
	}
}

func (s *serverBase) startExternalIPPoller(ctx context.Context) {
	s.fetchExternalIP(ctx)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.fetchExternalIP(ctx)
			}
		}
	}()
}

func parseLogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelError
	}
}

func configureRouter(sc systemControllerBackend) http.Handler {
	handlers := getHandler(sc)
	e := echo.New()
	e.HTTPErrorHandler = ProblemDetailHTTPErrorHandler()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel()}))
	e.Logger = logger
	slog.SetDefault(logger)
	e.Use(middleware.RequestLogger())
	allowedHosts := sc.GetAllowedHosts()
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		UnsafeAllowOriginFunc: func(_ *echo.Context, origin string) (string, bool, error) {
			if os.Getenv("DEBUG") != "" {
				return origin, true, nil
			}
			u, err := url.Parse(origin)
			if err != nil {
				return "", false, nil //nolint:nilerr // invalid origin is rejected, not an error
			}
			host := u.Hostname()
			for _, h := range allowedHosts {
				if strings.EqualFold(host, h) {
					return origin, true, nil
				}
			}
			return "", false, nil
		},
		AllowMethods:     []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
	}))
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Request().Header.Get("Access-Control-Request-Private-Network") == "true" {
				c.Response().Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			return next(c)
		}
	})
	e.Use(handlers.auditMiddleware)
	handlers.configureRoutes(e)
	return e
}

// NewHandler creates an http.Handler for the given ServerConfig.
// The system hostname is automatically added to AllowedHosts.
func NewHandler(cfg ServerConfig) http.Handler {
	cfg.AllowedHosts = append(cfg.AllowedHosts, "localhost")
	if hostname, err := os.Hostname(); err == nil {
		cfg.AllowedHosts = append(cfg.AllowedHosts, hostname)
	}
	sb := &serverBase{ServerConfig: cfg}
	return configureRouter(sb)
}

// --- TestServer ---

type TestServer struct {
	serverBase

	Server *httptest.Server
	cancel context.CancelFunc
}

func InitTestServer(cfg ServerConfig) *TestServer {
	ts := &TestServer{}
	ts.ServerConfig = cfg
	ctx, cancel := context.WithCancel(context.Background())
	ts.cancel = cancel
	ts.Server = httptest.NewServer(withContext(ctx, configureRouter(ts)))
	return ts
}

func (ts *TestServer) Close() {
	ts.cancel()
	ts.Server.Close()
}

func (ts *TestServer) Run() error {
	ts.Server.Start()
	return nil
}

func (ts *TestServer) Client() (*SystemdClient, error) {
	return FromClient(ts.Server.Client(), ts.Server.URL)
}

// --- UnixServer ---

type UnixServer struct {
	serverBase

	Socket string
	server *http.Server
	cancel context.CancelFunc
}

func InitUnixServer(sock string, cfg ServerConfig) *UnixServer {
	us := &UnixServer{Socket: sock}
	us.ServerConfig = cfg
	return us
}

func (us *UnixServer) Close() error {
	if us.cancel != nil {
		us.cancel()
	}
	return us.server.Close()
}

func (us *UnixServer) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	us.cancel = cancel
	us.server = &http.Server{Handler: withContext(ctx, configureRouter(us)), ReadHeaderTimeout: 10 * time.Second}
	us.startExternalIPPoller(ctx)
	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %w", us.Socket, err)
	}
	return us.server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemdClient, error) {
	return InitClient(us.Socket)
}
