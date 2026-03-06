package systemcontroller

import (
	"encoding/json"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// SystemServiceEntry is a system service enriched with live unit status.
type SystemServiceEntry struct {
	systemd.UnitStatus

	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Image       string `json:"image"`
	Port        string `json:"port"`
}

// SetSystemServiceStatusRequest is the request body for POST /system-services/status.
type SetSystemServiceStatusRequest struct {
	Key    string              `json:"key"`
	Action systemd.StatusAction `json:"action"`
}

func (s *SystemControllerHandlers) listSystemServices(c *echo.Context) error {
	mon := s.Controller.GetMonitoring()
	if mon == nil {
		return c.JSON(200, []SystemServiceEntry{})
	}

	svcs := mon.SystemServices()
	sd := s.Controller.GetSystemdManager()

	unitStates := map[string]systemd.UnitStatus{}
	if sd != nil {
		units, err := sd.ListUnits(c.Request().Context())
		if err == nil {
			for _, u := range units {
				if systemd.IsSystemServiceUnit(u.Name) {
					unitStates[u.Name] = u
				}
			}
		}
	}

	entries := make([]SystemServiceEntry, len(svcs))
	for i, svc := range svcs {
		us := unitStates[svc.UnitName]
		entries[i] = SystemServiceEntry{
			UnitStatus:  us,
			Key:         svc.Key,
			DisplayName: svc.DisplayName,
			Image:       svc.Image,
			Port:        svc.Port,
		}
		// Ensure the Name field is always set even if no unit status found.
		if entries[i].Name == "" {
			entries[i].Name = svc.UnitName
		}
	}

	return c.JSON(200, entries)
}

func (s *SystemControllerHandlers) setSystemServiceStatus(c *echo.Context) error {
	mon := s.Controller.GetMonitoring()
	if mon == nil {
		return echo.NewHTTPError(404, "monitoring not configured")
	}

	var req SetSystemServiceStatusRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	// Reject enable/disable — system services are always enabled.
	if req.Action == systemd.Enable || req.Action == systemd.Disable {
		return echo.NewHTTPError(400, fmt.Sprintf("action %q is not allowed for system services", req.Action))
	}

	// Validate key against known services.
	svcs := mon.SystemServices()
	var unitName string
	for _, svc := range svcs {
		if svc.Key == req.Key {
			unitName = svc.UnitName
			break
		}
	}
	if unitName == "" {
		return echo.NewHTTPError(400, fmt.Sprintf("unknown system service key %q", req.Key))
	}

	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return echo.NewHTTPError(500, "systemd manager not available")
	}

	if err := sd.SetStatus(c.Request().Context(), unitName, req.Action); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("set status: %v", err))
	}

	return c.JSON(200, map[string]string{"status": "ok"})
}
