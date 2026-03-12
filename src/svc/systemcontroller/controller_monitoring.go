package systemcontroller

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
)

func (s *SystemControllerHandlers) monitoringStatus(c *echo.Context) error {
	mon := s.Controller.GetMonitoring()
	if mon == nil {
		return c.JSON(200, map[string]string{"status": "disabled"})
	}
	status := mon.Status(c.Request().Context())
	return c.JSON(200, status)
}

// grafanaProxy reverse proxies requests to the local Grafana instance,
// stripping the /monitoring/grafana prefix from the URL path. This endpoint
// bypasses authentication per the functional specification.
func (s *SystemControllerHandlers) grafanaProxy(c *echo.Context) error {
	mon := s.Controller.GetMonitoring()
	if mon == nil {
		return c.String(http.StatusServiceUnavailable, "monitoring is not configured")
	}

	status := mon.Status(c.Request().Context())
	target, err := url.Parse("http://localhost:" + status.Grafana.Port)
	if err != nil {
		return c.String(http.StatusInternalServerError, "invalid grafana target")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	req := c.Request()
	req.URL.Path = strings.TrimPrefix(req.URL.Path, "/monitoring/grafana")
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}
	req.Host = target.Host
	proxy.ServeHTTP(c.Response(), req)
	return nil
}
