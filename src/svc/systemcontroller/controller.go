package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type FilesystemName struct {
	Name string `json:"name"`
}

type SystemController struct {
	Storage storage.Storage
}

func (s *SystemController) Run(sock string) error {
	lis, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %v", sock, err)
	}

	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Add("POST", "/storage/add", s.addFilesystem)
	e.Add("POST", "/storage/remove", s.removeFilesystem)
	e.Add("GET", "/storage", s.listFilesystems)

	server := &http.Server{}
	server.Handler = e
	return server.Serve(lis)
}

func (s *SystemController) addFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := storage.Filesystem{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if err := s.Storage.CreateFilesystem(fs); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemController) removeFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if err := s.Storage.RemoveFilesystem(fs.Name); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemController) listFilesystems(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	list, err := s.Storage.ListFilesystems(fs.Name)

	if err != nil {
		return err
	}

	return c.JSON(200, list)
}
