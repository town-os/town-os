package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"

	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type SystemController interface {
	Run() error
	GetStorage() storage.Storage
	Client() (*SystemClient, error)
}

type FilesystemName struct {
	Name string `json:"name"`
}

type SystemControllerHandlers struct {
	controller SystemController
}

func getHandler(sc SystemController) *SystemControllerHandlers {
	return &SystemControllerHandlers{controller: sc}
}

func (s *SystemControllerHandlers) createFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := storage.Filesystem{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if err := s.controller.GetStorage().CreateFilesystem(fs); err != nil {
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

	if err := s.controller.GetStorage().RemoveFilesystem(fs.Name); err != nil {
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

	list, err := s.controller.GetStorage().ListFilesystems(fs.Name)
	if err != nil {
		return err
	}

	return c.JSON(200, list)
}

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	e.Add("POST", "/storage/create", s.createFilesystem)
	e.Add("POST", "/storage/remove", s.removeFilesystem)
	e.Add("POST", "/storage", s.listFilesystems)
}

type TestServer struct {
	Storage storage.Storage
	Server  *httptest.Server
}

func InitTestServer(s storage.Storage) *TestServer {
	ts := &TestServer{Storage: s}
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

func (ts *TestServer) Run() error {
	ts.Server.Start()
	return nil
}

func (ts *TestServer) Client() (*SystemClient, error) {
	return FromClient(ts.Server.Client(), ts.Server.URL)
}

type UnixServer struct {
	Socket  string
	Storage storage.Storage
	Handler http.Handler
}

func InitUnixServer(sock string, s storage.Storage) *UnixServer {
	us := &UnixServer{Socket: sock, Storage: s}
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

	server := &http.Server{}
	server.Handler = us.ConfigureRouter()

	return server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemClient, error) {
	return InitClient(us.Socket)
}

func (us *UnixServer) GetStorage() storage.Storage {
	return us.Storage
}

func (us *UnixServer) ConfigureRouter() http.Handler {
	handlers := getHandler(us)

	e := echo.New()
	e.Use(middleware.RequestLogger())
	handlers.configureRoutes(e)

	return e
}
