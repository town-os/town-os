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
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type SystemController interface {
	Run() error
	GetStorage() storage.Storage
	GetRepositoryRoot() *packages.RepositoryRoot
	Client() (*SystemClient, error)
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

type PackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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

	out := make([]RepositoryInfo, len(rr.Items))
	for i, r := range rr.Items {
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

	out := make([]PackageInfo, len(pkgs))
	for i, p := range pkgs {
		out[i] = PackageInfo{Name: p.Name, Version: p.Version}
	}

	return c.JSON(200, out)
}

// --- Routes ---

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	e.Add("POST", "/storage/create", s.createFilesystem)
	e.Add("POST", "/storage/modify", s.modifyFilesystem)
	e.Add("POST", "/storage/remove", s.removeFilesystem)
	e.Add("POST", "/storage", s.listFilesystems)

	e.Add("POST", "/repository/add", s.addRepository)
	e.Add("POST", "/repository/remove", s.removeRepository)
	e.Add("POST", "/repository", s.listRepositories)

	e.Add("POST", "/packages", s.listPackages)
}

// --- TestServer ---

type TestServer struct {
	Storage        storage.Storage
	RepositoryRoot *packages.RepositoryRoot
	Server         *httptest.Server
}

func InitTestServer(s storage.Storage, rr *packages.RepositoryRoot) *TestServer {
	ts := &TestServer{Storage: s, RepositoryRoot: rr}
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

func (ts *TestServer) Run() error {
	ts.Server.Start()
	return nil
}

func (ts *TestServer) Client() (*SystemClient, error) {
	return FromClient(ts.Server.Client(), ts.Server.URL)
}

// --- UnixServer ---

type UnixServer struct {
	Socket         string
	Storage        storage.Storage
	RepositoryRoot *packages.RepositoryRoot
	Handler        http.Handler
}

func InitUnixServer(sock string, s storage.Storage, rr *packages.RepositoryRoot) *UnixServer {
	us := &UnixServer{Socket: sock, Storage: s, RepositoryRoot: rr}
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

func (us *UnixServer) Client() (*SystemClient, error) {
	return InitClient(us.Socket)
}

func (us *UnixServer) GetStorage() storage.Storage {
	return us.Storage
}

func (us *UnixServer) GetRepositoryRoot() *packages.RepositoryRoot {
	return us.RepositoryRoot
}
