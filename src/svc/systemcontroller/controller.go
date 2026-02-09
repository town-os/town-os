package systemcontroller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type SystemController interface {
	Run() error
	GetStorage() storage.Storage
	Client() (*SystemClient, error)
	ConfigureRouter() http.Handler
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

func (s *SystemControllerHandlers) addFilesystem(c *echo.Context) error {
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

func (ts *TestServer) ConfigureRouter() http.Handler {
	handlers := getHandler(ts)
	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Add("POST", "/storage/add", handlers.addFilesystem)
	e.Add("POST", "/storage/remove", handlers.removeFilesystem)
	e.Add("GET", "/storage", handlers.listFilesystems)
	return e
}

type TestServer struct {
	Storage storage.Storage
	Server  *httptest.Server
}

func (ts *TestServer) GetStorage() storage.Storage {
	return ts.Storage
}

func (ts *TestServer) Run() error {
	ts.Server.Start()
	return nil
}

func (ts *TestServer) Client() (*SystemClient, error) {
	return FromClient(ts.Server.Client())
}

/*
type UnixServer struct {
	Socket  string
	Storage storage.Storage
	Handler http.Handler
}

func (us *UnixServer) Run() error {
	lis, err := net.Listen("unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %v", us.Socket, err)
	}

	server := &http.Server{}
	server.Handler = us.Handler
	return server.Serve(lis)
}

func (s *SystemController) Run(sock string) error {
	return CreateUnixServer(sock, e).Start()
}
*/
