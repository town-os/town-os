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
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type serverBase struct {
	ServerConfig

	externalIP atomic.Value // stores string
	internalIP atomic.Value // stores string
}

func (s *serverBase) GetStorage() storage.Storage                 { return s.Storage }
func (s *serverBase) GetRepositoryRoot() *packages.RepositoryRoot { return s.RepositoryRoot }
func (s *serverBase) GetInstaller() packages.Installer            { return s.Installer }
func (s *serverBase) GetSystemdManager() systemd.Manager          { return s.Systemd }
func (s *serverBase) GetAccountManager() account.Manager          { return s.AccountMgr }
func (s *serverBase) GetSessionManager() account.SessionManager   { return s.SessionMgr }
func (s *serverBase) GetAuditManager() account.AuditManager       { return s.AuditMgr }
func (s *serverBase) GetSettingsManager() account.SettingsManager { return s.SettingsMgr }
func (s *serverBase) GetGitClient() git.Client                    { return s.Git }
func (s *serverBase) GetAllowedHosts() []string                   { return s.AllowedHosts }
func (s *serverBase) GetDefaultRepoCredentials() (string, string) {
	return s.DefaultRepoUser, s.DefaultRepoPass
}
func (s *serverBase) GetBtrfsBasePath() string          { return s.BtrfsBasePath }
func (s *serverBase) GetNetworkControllerImage() string { return s.NetworkControllerImage }
func (s *serverBase) GetNetworkStatePath() string       { return s.NetworkStatePath }
func (s *serverBase) GetGitCloner() packages.GitCloner {
	if s.GitCloner != nil {
		return s.GitCloner
	}
	return packages.DefaultGitCloner{}
}
func (s *serverBase) GetPagesManager() account.PagesManager { return s.PagesMgr }
func (s *serverBase) GetMonitoringBackend() string { return s.MonitoringBackend }

// RefreshMonitoringBackend switches the monitoring UI to the given backend
// by regenerating and restarting the monitoring-ui system service. The
// MonitoringBackend field is updated so subsequent reads reflect the change.
func (s *serverBase) RefreshMonitoringBackend(ctx context.Context, backend string) error {
	s.MonitoringBackend = backend
	sd := s.GetSystemdManager()
	if sd == nil {
		return nil
	}
	return monitoring.StartMonitoringUI(ctx, sd, backend, s.BtrfsBasePath, s.NetworkControllerImage, s.NetworkStatePath)
}
func (s *serverBase) GetRolodex() *rolodex.Manager           { return s.Rolodex }
func (s *serverBase) GetUI() *ui.Manager                     { return s.UI }
func (s *serverBase) GetResolvedConfigurator() func(ctx context.Context, tld, loopbackAddr string) {
	return s.ResolvedConfigurator
}
func (s *serverBase) GetSystemControllerImage() string { return s.SystemControllerImage }
func (s *serverBase) GetSystemControllerListenAddr() string  { return s.SystemControllerListenAddr }
func (s *serverBase) GetImageExtractFunc() func(ctx context.Context, image, directory, targetPath string) error {
	if s.ImageExtractFunc != nil {
		return s.ImageExtractFunc
	}
	return reconcileExtractFromImage
}
func (s *serverBase) GetRolodexClient() rolodex.Client {
	if s.RolodexClient != nil {
		return s.RolodexClient
	}
	if s.Rolodex == nil {
		return nil
	}
	socketPath := s.Rolodex.SocketPath()
	if socketPath == "" {
		return nil
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, err := rolodex.Dial(dialCtx, socketPath)
	if err != nil {
		slog.Debug(fmt.Sprintf("lazy rolodex dial: %v", err))
		return nil
	}
	s.RolodexClient = c
	return c
}
func (s *serverBase) GetExternalIP() string {
	v := s.externalIP.Load()
	if v == nil {
		return ""
	}
	if ip, ok := v.(string); ok {
		return ip
	}
	return ""
}

func (s *serverBase) GetInternalIP() string {
	v := s.internalIP.Load()
	if v == nil {
		s.RefreshInternalIP()
		v = s.internalIP.Load()
		if v == nil {
			return ""
		}
	}
	if ip, ok := v.(string); ok {
		return ip
	}
	return ""
}

// RefreshInternalIP updates the cached internal IP address by querying
// the system's network interfaces.
func (s *serverBase) RefreshInternalIP() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			s.internalIP.Store(ipnet.IP.String())
			return
		}
	}
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

func configureRouter(ctx context.Context, sc systemControllerBackend) http.Handler {
	handlers := getHandler(ctx, sc)
	e := echo.New()
	e.HTTPErrorHandler = ProblemDetailHTTPErrorHandler()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel()}))
	e.Logger = logger
	slog.SetDefault(logger)
	e.Use(middleware.RequestLogger())
	allowedHosts := sc.GetAllowedHosts()
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		UnsafeAllowOriginFunc: func(c *echo.Context, origin string) (string, bool, error) {
			if os.Getenv("DEBUG") != "" {
				return origin, true, nil
			}
			u, err := url.Parse(origin)
			if err != nil {
				return "", false, nil //nolint:nilerr // invalid origin is rejected, not an error
			}
			originHost := u.Hostname()

			// Allow cross-port requests from the same hostname the
			// browser used to reach this server.
			if reqHost := c.Request().Host; reqHost != "" {
				if h, _, err := net.SplitHostPort(reqHost); err == nil {
					reqHost = h
				}
				if strings.EqualFold(originHost, reqHost) {
					return origin, true, nil
				}
			}

			for _, h := range allowedHosts {
				if strings.EqualFold(originHost, h) {
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
// The external IP poller is started in the background using the provided context.
func NewHandler(ctx context.Context, cfg ServerConfig) http.Handler {
	cfg.AllowedHosts = append(cfg.AllowedHosts, "localhost")
	if hostname, err := os.Hostname(); err == nil {
		cfg.AllowedHosts = append(cfg.AllowedHosts, hostname)
	}
	sb := &serverBase{ServerConfig: cfg}
	sb.startExternalIPPoller(ctx)
	return configureRouter(ctx, sb)
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
	ts.Server = httptest.NewServer(withContext(ctx, configureRouter(ctx, ts)))
	return ts
}

func (ts *TestServer) Close() {
	ts.cancel()
	ts.Server.Close()
}

// SetExternalIP stores an external IP for testing. This allows integration
// tests to verify the ping response includes the external IP without
// depending on a live ipinfo.io fetch.
func (ts *TestServer) SetExternalIP(ip string) {
	ts.externalIP.Store(ip)
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
	us.server = &http.Server{Handler: withContext(ctx, configureRouter(ctx, us)), ReadHeaderTimeout: 10 * time.Second}
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
