package systemcontroller

import (
	"fmt"
	"net"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func Init(sock string) error {
	lis, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %v", sock, err)
	}

	e := echo.New()
	e.Use(middleware.RequestLogger())
	server := &http.Server{}
	server.Handler = e
	return server.Serve(lis)
}
