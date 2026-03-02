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

func (s *SystemControllerHandlers) monitoringGrafanaProxy(c *echo.Context) error {
	mon := s.Controller.GetMonitoring()
	if mon == nil {
		return echo.NewHTTPError(503, "monitoring is not configured")
	}

	grafanaURL := mon.GrafanaURL()
	target, err := url.Parse(grafanaURL)
	if err != nil {
		return echo.NewHTTPError(500, "invalid grafana URL")
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			// Strip the /monitoring/grafana prefix and forward the rest.
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/monitoring/grafana")
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
			req.Host = target.Host
		},
	}

	proxy.ServeHTTP(c.Response(), c.Request()) //nolint:gosec // G704: target URL is constructed from internal config, not user input
	return nil
}
