package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type SystemController interface {
	Run() error
	GetStorage() storage.Storage
	GetRepositoryRoot() *packages.RepositoryRoot
	GetInstaller() packages.Installer
	GetSystemdManager() systemd.Manager
	GetAccountManager() account.Manager
	GetSessionManager() account.SessionManager
	Client() (*SystemdClient, error)
}

type FilesystemName struct {
	Name string `json:"name"`
}

type ModifyFilesystemRequest struct {
	Name       string             `json:"name"`
	Filesystem storage.Filesystem `json:"filesystem"`
}

type AddRepositoryRequest struct {
	URL string `json:"url"`
}

type RepositoryNameRequest struct {
	Name string `json:"name"`
}

type RepositoryInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type PackageNameRequest struct {
	Name string `json:"name"`
}

type InstallRequest struct {
	Name      string             `json:"name"`
	Version   string             `json:"version"`
	Responses packages.Responses `json:"responses"`
}

type UninstallRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type GetResponsesRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type SetStatusRequest struct {
	Name   string               `json:"name"`
	Action systemd.StatusAction `json:"action"`
}

type CreateAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	RealName string `json:"real_name"`
	Admin    bool   `json:"admin"`
}

type GetAccountRequest struct {
	Username string `json:"username"`
}

type UpdateAccountRequest struct {
	Username string              `json:"username"`
	Fields   account.UpdateFields `json:"fields"`
}

type DeleteAccountRequest struct {
	Username string `json:"username"`
}

type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthenticateResponse struct {
	Token   string           `json:"token"`
	Account *account.Account `json:"account"`
}

type RevokeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type SessionUsernameResponse struct {
	Username string `json:"username"`
}

type PingResponse struct {
	Status       string      `json:"status"`
	Filesystems  int         `json:"filesystems"`
	Repositories int         `json:"repositories"`
	Packages     int         `json:"packages"`
	Installed    int         `json:"installed"`
	Accounts     int         `json:"accounts"`
	Units        *UnitCounts `json:"units,omitempty"`
}

type UnitCounts struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Failed int `json:"failed"`
}

type SystemControllerHandlers struct {
	Controller SystemController
}

func getHandler(sc SystemController) *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: sc}
}

// --- Storage handlers ---

func (s *SystemControllerHandlers) createFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := storage.Filesystem{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if err := s.Controller.GetStorage().CreateFilesystem(fs); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) removeFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if err := s.Controller.GetStorage().RemoveFilesystem(fs.Name); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) modifyFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := ModifyFilesystemRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetStorage().ModifyFilesystem(req.Name, req.Filesystem); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listFilesystems(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	list, err := s.Controller.GetStorage().ListFilesystems(fs.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, list)
}

// --- Repository handlers ---

func (s *SystemControllerHandlers) addRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := AddRepositoryRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	u, err := url.Parse(req.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %v", err)
	}

	rr := s.Controller.GetRepositoryRoot()

	repo, err := packages.NewRepository(rr.BaseDir, *u)
	if err != nil {
		return err
	}

	if err := rr.Add(*repo); err != nil {
		return err
	}

	if err := rr.Refresh(); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) removeRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RepositoryNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	if err := rr.Remove(req.Name); err != nil {
		return err
	}

	if err := rr.Refresh(); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listRepositories(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	repos, err := rr.List()
	if err != nil {
		return err
	}

	out := make([]RepositoryInfo, len(repos))
	for i, r := range repos {
		out[i] = RepositoryInfo{Name: r.Name, URL: r.URL.String()}
	}

	return c.JSON(200, out)
}

// --- Package handlers ---

func (s *SystemControllerHandlers) listPackages(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	pkgs, err := rr.ListPackages()
	if err != nil {
		return err
	}

	return c.JSON(200, pkgs)
}

func (s *SystemControllerHandlers) getPackageQuestions(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	questions, err := rr.GetPackageQuestions(req.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, questions)
}

// --- Install handlers ---

func (s *SystemControllerHandlers) installPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := InstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()
	repoName, err := rr.FindRepoForPackage(req.Name, req.Version)
	if err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.Install(repoName, req.Name, req.Version, req.Responses); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) uninstallPackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UninstallRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.Uninstall(req.Name, req.Version); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listInstalled(c *echo.Context) error {
	inst := s.Controller.GetInstaller()

	pkgs, err := inst.ListInstalled()
	if err != nil {
		return err
	}

	return c.JSON(200, pkgs)
}

func (s *SystemControllerHandlers) getResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetResponsesRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	resp, err := inst.GetResponses(req.Name, req.Version)
	if err != nil {
		return err
	}

	return c.JSON(200, resp)
}

// --- Systemd handlers ---

func (s *SystemControllerHandlers) listUnits(c *echo.Context) error {
	units, err := s.Controller.GetSystemdManager().ListUnits(c.Request().Context())
	if err != nil {
		return err
	}

	return c.JSON(200, units)
}

func (s *SystemControllerHandlers) setUnitStatus(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := SetStatusRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetSystemdManager().SetStatus(c.Request().Context(), req.Name, req.Action); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) logReplay(c *echo.Context) error {
	unit := c.QueryParam("unit")
	if unit == "" {
		return fmt.Errorf("missing unit query parameter")
	}

	ch, err := s.Controller.GetSystemdManager().LogReplay(c.Request().Context(), unit)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().WriteHeader(200)

	flusher, ok := c.Response().(http.Flusher)
	ctx := c.Request().Context()
	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case entry, open := <-ch:
			if !open {
				return nil
			}
			if _, err := fmt.Fprint(c.Response(), "data: "); err != nil {
				return err
			}
			if err := json.NewEncoder(c.Response()).Encode(entry); err != nil {
				return err
			}
			if _, err := fmt.Fprint(c.Response(), "\n"); err != nil {
				return err
			}
			if ok {
				flusher.Flush()
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Response(), ":\n"); err != nil {
				return err
			}
			if ok {
				flusher.Flush()
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// --- Account handlers ---

func (s *SystemControllerHandlers) createAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := CreateAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Create(req.Username, req.Password, req.Email, req.Phone, req.RealName, req.Admin)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) getAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Get(req.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) updateAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UpdateAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Update(req.Username, req.Fields)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) deleteAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DeleteAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetAccountManager().Delete(req.Username); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listAccounts(c *echo.Context) error {
	accounts, err := s.Controller.GetAccountManager().List()
	if err != nil {
		return err
	}

	return c.JSON(200, accounts)
}

func (s *SystemControllerHandlers) authenticateAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := AuthenticateRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Authenticate(req.Username, req.Password)
	if err != nil {
		return err
	}

	token, err := s.Controller.GetSessionManager().Create(req.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, AuthenticateResponse{Token: token, Account: acct})
}

func (s *SystemControllerHandlers) revokeSession(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RevokeSessionRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetSessionManager().Revoke(req.SessionID); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listSessions(c *echo.Context) error {
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, "missing authorization token")
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, "invalid session")
	}

	sessions, err := s.Controller.GetSessionManager().List(sess.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, sessions)
}

func (s *SystemControllerHandlers) sessionUsername(c *echo.Context) error {
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, "missing authorization token")
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, "invalid session")
	}

	return c.JSON(200, SessionUsernameResponse{Username: sess.Username})
}

// --- Admin middleware ---

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

func (s *SystemControllerHandlers) requireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, "missing authorization token")
		}

		_, acct, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, "invalid session")
		}

		if !acct.Admin {
			return echo.NewHTTPError(403, "admin access required")
		}

		return next(c)
	}
}

// --- Status handlers ---

func (s *SystemControllerHandlers) ping(c *echo.Context) error {
	resp := PingResponse{Status: "ok"}

	if st := s.Controller.GetStorage(); st != nil {
		fs, err := st.ListFilesystems("")
		if err != nil {
			return err
		}
		resp.Filesystems = len(fs)
	}

	if rr := s.Controller.GetRepositoryRoot(); rr != nil {
		repos, err := rr.List()
		if err != nil {
			return err
		}
		resp.Repositories = len(repos)

		pkgs, err := rr.ListPackages()
		if err != nil {
			return err
		}
		resp.Packages = len(pkgs)
	}

	if inst := s.Controller.GetInstaller(); inst != nil {
		installed, err := inst.ListInstalled()
		if err != nil {
			return err
		}
		resp.Installed = len(installed)
	}

	if am := s.Controller.GetAccountManager(); am != nil {
		accounts, err := am.List()
		if err != nil {
			return err
		}
		resp.Accounts = len(accounts)
	}

	if sd := s.Controller.GetSystemdManager(); sd != nil {
		units, err := sd.ListUnits(c.Request().Context())
		if err != nil {
			return err
		}
		counts := &UnitCounts{Total: len(units)}
		for _, u := range units {
			switch u.ActiveState {
			case "active":
				counts.Active++
			case "failed":
				counts.Failed++
			}
		}
		resp.Units = counts
	}

	return c.JSON(200, resp)
}

// --- Routes ---

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	e.Add("GET", "/status/ping", s.ping)

	e.Add("POST", "/storage/create", s.createFilesystem)
	e.Add("POST", "/storage/modify", s.modifyFilesystem)
	e.Add("POST", "/storage/remove", s.removeFilesystem)
	e.Add("POST", "/storage", s.listFilesystems)

	e.Add("POST", "/repository/add", s.addRepository)
	e.Add("POST", "/repository/remove", s.removeRepository)
	e.Add("GET", "/repository", s.listRepositories)

	e.Add("GET", "/packages", s.listPackages)
	e.Add("POST", "/packages/questions", s.getPackageQuestions, s.requireAdmin)
	e.Add("POST", "/packages/install", s.installPackage, s.requireAdmin)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage, s.requireAdmin)
	e.Add("GET", "/packages/installed", s.listInstalled)
	e.Add("POST", "/packages/responses", s.getResponses)

	e.Add("GET", "/systemd/units", s.listUnits)
	e.Add("POST", "/systemd/status", s.setUnitStatus, s.requireAdmin)
	e.Add("GET", "/systemd/logs", s.logReplay)

	e.Add("POST", "/account/create", s.createAccount)
	e.Add("POST", "/account", s.getAccount)
	e.Add("POST", "/account/update", s.updateAccount)
	e.Add("POST", "/account/delete", s.deleteAccount)
	e.Add("GET", "/account", s.listAccounts)
	e.Add("POST", "/account/authenticate", s.authenticateAccount)
	e.Add("POST", "/account/session/revoke", s.revokeSession)
	e.Add("GET", "/account/sessions", s.listSessions)
	e.Add("GET", "/account/session/username", s.sessionUsername)
}

// --- Server infrastructure ---

type ServerConfig struct {
	Storage        storage.Storage
	RepositoryRoot *packages.RepositoryRoot
	Installer      packages.Installer
	Systemd        systemd.Manager
	AccountMgr     account.Manager
	SessionMgr     account.SessionManager
}

type contextHandler struct {
	ctx     context.Context
	handler http.Handler
}

func (h *contextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	h.handler.ServeHTTP(w, r.WithContext(ctx))
}

type serverBase struct {
	ServerConfig
	cancel context.CancelFunc
}

func (s *serverBase) GetStorage() storage.Storage                 { return s.Storage }
func (s *serverBase) GetRepositoryRoot() *packages.RepositoryRoot { return s.RepositoryRoot }
func (s *serverBase) GetInstaller() packages.Installer            { return s.Installer }
func (s *serverBase) GetSystemdManager() systemd.Manager          { return s.Systemd }
func (s *serverBase) GetAccountManager() account.Manager          { return s.AccountMgr }
func (s *serverBase) GetSessionManager() account.SessionManager   { return s.SessionMgr }

func configureRouter(sc SystemController) http.Handler {
	handlers := getHandler(sc)
	e := echo.New()
	e.Use(middleware.RequestLogger())
	handlers.configureRoutes(e)
	return e
}

// --- TestServer ---

type TestServer struct {
	serverBase
	Server *httptest.Server
}

func InitTestServer(cfg ServerConfig) *TestServer {
	ts := &TestServer{}
	ts.ServerConfig = cfg
	ctx, cancel := context.WithCancel(context.Background())
	ts.cancel = cancel
	ts.Server = httptest.NewServer(&contextHandler{ctx: ctx, handler: configureRouter(ts)})
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
}

func InitUnixServer(sock string, cfg ServerConfig) *UnixServer {
	us := &UnixServer{Socket: sock}
	us.ServerConfig = cfg
	ctx, cancel := context.WithCancel(context.Background())
	us.cancel = cancel
	us.server = &http.Server{Handler: &contextHandler{ctx: ctx, handler: configureRouter(us)}}
	return us
}

func (us *UnixServer) Close() error {
	us.cancel()
	return us.server.Close()
}

func (us *UnixServer) Run() error {
	lis, err := net.Listen("unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %v", us.Socket, err)
	}
	return us.server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemdClient, error) {
	return InitClient(us.Socket)
}
