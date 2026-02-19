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
	"sort"
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
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
	GetAllowedHosts() []string
	GetDefaultRepoCredentials() (string, string)
}

type SystemController interface {
	systemControllerBackend
	Run() error
	Client() (*SystemdClient, error)
}

type FilesystemName struct {
	Name      string `json:"name"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
}

type ModifyFilesystemRequest struct {
	Name       string             `json:"name"`
	Filesystem storage.Filesystem `json:"filesystem"`
}

type AddRepositoryRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type RepositoryNameRequest struct {
	Name string `json:"name"`
}

type RepositoryInfo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Error    string `json:"error,omitempty"`
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
	Username string               `json:"username"`
	Fields   account.UpdateFields `json:"fields"`
}

type DisableAccountRequest struct {
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
	RecentErrors int         `json:"recent_errors"`
}

type UnitCounts struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Failed int `json:"failed"`
}

type SystemControllerHandlers struct {
	Controller systemControllerBackend
}

func getHandler(sc systemControllerBackend) *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: sc}
}

// readSortParams extracts sort_by and sort_order from GET query parameters.
func readSortParams(c *echo.Context) (string, string) {
	return c.QueryParam("sort_by"), c.QueryParam("sort_order")
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

	if fs.Name == "" {
		return storage.ErrRootFilesystem
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

	filtered := make([]storage.Filesystem, 0, len(list))
	for _, f := range list {
		if f.Name != "" {
			filtered = append(filtered, f)
		}
	}

	sortSlice(filtered, fs.SortBy, fs.SortOrder)

	return c.JSON(200, filtered)
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

	if req.Username == "" && req.Password == "" {
		req.Username, req.Password = s.Controller.GetDefaultRepoCredentials()
	}

	rr := s.Controller.GetRepositoryRoot()

	repo, err := packages.NewRepository(rr.BaseDir, req.Name, *u, req.Username, req.Password)
	if err != nil {
		return err
	}

	if err := rr.Add(*repo); err != nil {
		return err
	}

	rr.Refresh()

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

	rr.Refresh()

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) refreshRepositories(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()
	rr.Refresh()
	errs := rr.RefreshErrors()
	if len(errs) > 0 {
		return c.JSON(200, errs)
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

	errs := rr.RefreshErrors()
	out := make([]RepositoryInfo, len(repos))
	for i, r := range repos {
		out[i] = RepositoryInfo{Name: r.Name, URL: r.URL.String(), Username: r.Username, Error: errs[r.Name]}
	}

	p := readListParams(c)
	out = filterSearch(out, p.Search)
	sortSlice(out, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(out, p.Limit, p.Offset))
}

// --- Package handlers ---

func (s *SystemControllerHandlers) listPackages(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	pkgs, err := rr.ListPackages()
	if err != nil {
		return err
	}

	p := readListParams(c)
	pkgs = filterSearch(pkgs, p.Search)
	sort.Strings(pkgs)
	if strings.EqualFold(p.SortOrder, "desc") {
		sort.Sort(sort.Reverse(sort.StringSlice(pkgs)))
	}

	return c.JSON(200, paginate(pkgs, p.Limit, p.Offset))
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

	ctx := c.Request().Context()
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		unitName := systemd.UnitName(req.Name)
		content := systemd.StubUnitContent(req.Name, req.Version)
		if err := sd.InstallUnit(ctx, unitName, content); err != nil {
			return err
		}
		if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
			return err
		}
		if err := sd.SetStatus(ctx, unitName, systemd.Start); err != nil {
			return err
		}
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

	ctx := c.Request().Context()
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		unitName := systemd.UnitName(req.Name)
		if err := sd.SetStatus(ctx, unitName, systemd.Stop); err != nil {
			return err
		}
		if err := sd.SetStatus(ctx, unitName, systemd.Disable); err != nil {
			return err
		}
		if err := sd.UninstallUnit(ctx, unitName); err != nil {
			return err
		}
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

	p := readListParams(c)
	pkgs = filterSearch(pkgs, p.Search)
	sort.Strings(pkgs)
	if strings.EqualFold(p.SortOrder, "desc") {
		sort.Sort(sort.Reverse(sort.StringSlice(pkgs)))
	}

	return c.JSON(200, paginate(pkgs, p.Limit, p.Offset))
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

	p := readListParams(c)
	units = filterSearch(units, p.Search)
	sortSlice(units, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(units, p.Limit, p.Offset))
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

func (s *SystemControllerHandlers) logTail(c *echo.Context) error {
	unit := c.QueryParam("unit")
	if unit == "" {
		return fmt.Errorf("missing unit query parameter")
	}

	lines := 100
	if v := c.QueryParam("lines"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid lines parameter: %w", err)
		}
		lines = n
	}

	params := systemd.LogTailParams{
		Unit:         unit,
		Lines:        lines,
		BeforeCursor: c.QueryParam("before"),
		AfterCursor:  c.QueryParam("after"),
		Grep:         c.QueryParam("grep"),
	}

	if v := c.QueryParam("since"); v != "" {
		sinceUnix, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid since parameter: %w", err)
		}
		params.Since = time.Unix(sinceUnix, 0)
	}

	result, err := s.Controller.GetSystemdManager().LogTail(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(200, result)
}

// --- Account handlers ---

func (s *SystemControllerHandlers) createAccount(c *echo.Context) error {
	sessMgr := s.Controller.GetSessionManager()
	if sessMgr != nil {
		accounts, err := s.Controller.GetAccountManager().List()
		if err != nil {
			return fmt.Errorf("list accounts: %w", err)
		}

		hasEnabled := false
		for _, a := range accounts {
			if !a.Disabled {
				hasEnabled = true
				break
			}
		}

		if hasEnabled {
			token := extractBearerToken(c.Request())
			if token == "" {
				return echo.NewHTTPError(401, "missing authorization token")
			}
			_, acct, err := sessMgr.Validate(token)
			if err != nil {
				return echo.NewHTTPError(401, "invalid session")
			}
			if !acct.Admin {
				return echo.NewHTTPError(403, "admin access required")
			}
		}
	}

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

func (s *SystemControllerHandlers) disableAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DisableAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetAccountManager().Disable(req.Username); err != nil {
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

	sortBy, sortOrder := readSortParams(c)
	sortSlice(accounts, sortBy, sortOrder)

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

func (s *SystemControllerHandlers) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, "missing authorization token")
		}

		_, _, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, "invalid session")
		}

		return next(c)
	}
}

func (s *SystemControllerHandlers) auditMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		am := s.Controller.GetAuditManager()
		if am == nil {
			return next(c)
		}

		path := c.Request().URL.Path

		excluded := map[string]bool{
			"/account/sessions":         true,
			"/account/me":               true,
			"/status/ping":              true,
			"/audit/log":                true,
			"/storage":                  true,
			"/repository":               true,
			"/packages":                 true,
			"/packages/installed":       true,
			"/packages/responses":       true,
			"/packages/questions":       true,
			"/systemd/units":            true,
			"/systemd/logs":             true,
			"/systemd/logs/tail":        true,
			"/account":                  true,
		}

		if excluded[path] {
			return next(c)
		}

		handlerErr := next(c)

		var acctName string
		if path == "/account/authenticate" {
			if handlerErr == nil {
				// On success, extract username from request body - but body is consumed.
				// We need to extract from the auth request. Since body is consumed,
				// we extract from the response or use request data.
				// The authenticate handler already ran; we can try to get the username
				// from the request by re-reading. But the body is consumed.
				// Instead, just use the token from context - but authenticate doesn't set context.
				// Best approach: parse the username from the original request.
				// Since the body is consumed by the handler, we need another approach.
				// We'll leave account empty for authenticate on success - the audit log
				// will still record the action.
				acctName = ""
			}
		} else {
			token := extractBearerToken(c.Request())
			if token != "" && s.Controller.GetSessionManager() != nil {
				_, acct, err := s.Controller.GetSessionManager().Validate(token)
				if err == nil {
					acctName = acct.Username
				}
			}
		}

		action := account.RouteActions[path]

		entry := account.AuditEntry{
			Account:   acctName,
			Action:    action,
			Path:      path,
			Success:   handlerErr == nil,
			CreatedAt: time.Now().UTC(),
		}
		if handlerErr != nil {
			entry.Error = handlerErr.Error()
		}

		// Best-effort audit logging; don't fail the request if logging fails
		_ = am.LogEntry(entry)

		return handlerErr
	}
}

func (s *SystemControllerHandlers) listAuditLog(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	var opts account.AuditListOptions

	if err := de.Decode(&opts); err != nil {
		return err
	}

	am := s.Controller.GetAuditManager()
	if am == nil {
		return echo.NewHTTPError(500, "audit logging not configured")
	}

	page, err := am.List(opts)
	if err != nil {
		return err
	}

	return c.JSON(200, page)
}

// --- Status handlers ---

func (s *SystemControllerHandlers) ping(c *echo.Context) error {
	resp := PingResponse{Status: "ok"}

	if st := s.Controller.GetStorage(); st != nil {
		fs, err := st.ListFilesystems("")
		if err != nil {
			return err
		}
		count := 0
		for _, f := range fs {
			if f.Name != "" {
				count++
			}
		}
		resp.Filesystems = count
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

	if am := s.Controller.GetAuditManager(); am != nil {
		n, err := am.CountRecentErrors(time.Now().Add(-5 * time.Minute))
		if err != nil {
			return err
		}
		resp.RecentErrors = n
	}

	return c.JSON(200, resp)
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
	e.Add("POST", "/repository/refresh", s.refreshRepositories, s.requireAuth)
	e.Add("GET", "/repository", s.listRepositories, s.requireAuth)

	e.Add("GET", "/packages", s.listPackages, s.requireAuth)
	e.Add("GET", "/packages/installed", s.listInstalled, s.requireAuth)
	e.Add("POST", "/packages/responses", s.getResponses, s.requireAuth)

	e.Add("GET", "/systemd/units", s.listUnits, s.requireAuth)
	e.Add("GET", "/systemd/logs", s.logReplay, s.requireAuth)
	e.Add("GET", "/systemd/logs/tail", s.logTail, s.requireAuth)

	e.Add("POST", "/account/create", s.createAccount)
	e.Add("POST", "/account", s.getAccount, s.requireAuth)
	e.Add("POST", "/account/update", s.updateAccount, s.requireAuth)
	e.Add("GET", "/account", s.listAccounts, s.requireAuth)

	// Admin (requireAdmin, which implies auth)
	e.Add("POST", "/packages/questions", s.getPackageQuestions, s.requireAdmin)
	e.Add("POST", "/packages/install", s.installPackage, s.requireAdmin)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage, s.requireAdmin)
	e.Add("POST", "/systemd/status", s.setUnitStatus, s.requireAdmin)
	e.Add("POST", "/account/disable", s.disableAccount, s.requireAdmin)
	e.Add("POST", "/audit/log", s.listAuditLog, s.requireAdmin)
}

// --- Server infrastructure ---

type ServerConfig struct {
	Storage            storage.Storage
	RepositoryRoot     *packages.RepositoryRoot
	Installer          packages.Installer
	Systemd            systemd.Manager
	AccountMgr         account.Manager
	SessionMgr         account.SessionManager
	AuditMgr           account.AuditManager
	AllowedHosts       []string
	DefaultRepoUser    string
	DefaultRepoPass    string
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
func (s *serverBase) GetAuditManager() account.AuditManager       { return s.AuditMgr }
func (s *serverBase) GetAllowedHosts() []string                   { return s.AllowedHosts }
func (s *serverBase) GetDefaultRepoCredentials() (string, string) {
	return s.DefaultRepoUser, s.DefaultRepoPass
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
				return "", false, nil
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
