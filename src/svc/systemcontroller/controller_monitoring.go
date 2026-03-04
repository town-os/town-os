package systemcontroller

import (
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
