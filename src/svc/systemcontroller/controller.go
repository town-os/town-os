package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"

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

	for entry := range ch {
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
	}

	return nil
}

// --- Routes ---

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	e.Add("POST", "/storage/create", s.createFilesystem)
	e.Add("POST", "/storage/modify", s.modifyFilesystem)
	e.Add("POST", "/storage/remove", s.removeFilesystem)
	e.Add("POST", "/storage", s.listFilesystems)

	e.Add("POST", "/repository/add", s.addRepository)
	e.Add("POST", "/repository/remove", s.removeRepository)
	e.Add("GET", "/repository", s.listRepositories)

	e.Add("GET", "/packages", s.listPackages)
	e.Add("POST", "/packages/questions", s.getPackageQuestions)
	e.Add("POST", "/packages/install", s.installPackage)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage)
	e.Add("GET", "/packages/installed", s.listInstalled)
	e.Add("POST", "/packages/responses", s.getResponses)

	e.Add("GET", "/systemd/units", s.listUnits)
	e.Add("POST", "/systemd/status", s.setUnitStatus)
	e.Add("GET", "/systemd/logs", s.logReplay)
}

// --- TestServer ---

type TestServer struct {
	Storage        storage.Storage
	RepositoryRoot *packages.RepositoryRoot
	Installer      packages.Installer
	Systemd        systemd.Manager
	Server         *httptest.Server
}

func InitTestServer(s storage.Storage, rr *packages.RepositoryRoot, inst packages.Installer, sd systemd.Manager) *TestServer {
	ts := &TestServer{Storage: s, RepositoryRoot: rr, Installer: inst, Systemd: sd}
	ts.Server = httptest.NewServer(configureTestRouter(ts))
	return ts
}

func configureTestRouter(sc SystemController) http.Handler {
	handlers := getHandler(sc)

	e := echo.New()

	if os.Getenv("DEBUG") != "" {
		e.Use(middleware.RequestLogger())
	}

	handlers.configureRoutes(e)

	return e
}

func (ts *TestServer) GetStorage() storage.Storage {
	return ts.Storage
}

func (ts *TestServer) GetRepositoryRoot() *packages.RepositoryRoot {
	return ts.RepositoryRoot
}

func (ts *TestServer) GetInstaller() packages.Installer {
	return ts.Installer
}

func (ts *TestServer) GetSystemdManager() systemd.Manager {
	return ts.Systemd
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
	Socket         string
	Storage        storage.Storage
	RepositoryRoot *packages.RepositoryRoot
	Installer      packages.Installer
	Systemd        systemd.Manager
	Handler        http.Handler
}

func InitUnixServer(sock string, s storage.Storage, rr *packages.RepositoryRoot, inst packages.Installer, sd systemd.Manager) *UnixServer {
	us := &UnixServer{Socket: sock, Storage: s, RepositoryRoot: rr, Installer: inst, Systemd: sd}
	us.Handler = configureUnixRouter(us)
	return us
}

func configureUnixRouter(sc SystemController) http.Handler {
	handlers := getHandler(sc)

	e := echo.New()
	e.Use(middleware.RequestLogger())
	handlers.configureRoutes(e)

	return e
}

func (us *UnixServer) Run() error {
	lis, err := net.Listen("unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %v", us.Socket, err)
	}

	server := &http.Server{Handler: us.Handler}

	return server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemdClient, error) {
	return InitClient(us.Socket)
}

func (us *UnixServer) GetStorage() storage.Storage {
	return us.Storage
}

func (us *UnixServer) GetRepositoryRoot() *packages.RepositoryRoot {
	return us.RepositoryRoot
}

func (us *UnixServer) GetInstaller() packages.Installer {
	return us.Installer
}

func (us *UnixServer) GetSystemdManager() systemd.Manager {
	return us.Systemd
}
