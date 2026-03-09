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

// systemServiceInfo is a unified view of a system service from any provider.
type systemServiceInfo struct {
	Key         string
	DisplayName string
	Image       string
	Port        string
	UnitName    string
}

// collectSystemServices gathers system service metadata from all providers.
func (s *SystemControllerHandlers) collectSystemServices() []systemServiceInfo {
	var all []systemServiceInfo

	if mon := s.Controller.GetMonitoring(); mon != nil {
		for _, svc := range mon.SystemServices() {
			all = append(all, systemServiceInfo{
				Key:         svc.Key,
				DisplayName: svc.DisplayName,
				Image:       svc.Image,
				Port:        svc.Port,
				UnitName:    svc.UnitName,
			})
		}
	}

	if rol := s.Controller.GetRolodex(); rol != nil {
		for _, svc := range rol.SystemServices() {
			all = append(all, systemServiceInfo{
				Key:         svc.Key,
				DisplayName: svc.DisplayName,
				Image:       svc.Image,
				Port:        svc.Port,
				UnitName:    svc.UnitName,
			})
		}
	}

	return all
}

func (s *SystemControllerHandlers) listSystemServices(c *echo.Context) error {
	svcs := s.collectSystemServices()
	if len(svcs) == 0 {
		return c.JSON(200, []SystemServiceEntry{})
	}

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
	svcs := s.collectSystemServices()
	if len(svcs) == 0 {
		return echo.NewHTTPError(404, "no system services configured")
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
