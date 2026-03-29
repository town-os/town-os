package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
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

	// Monitoring services are all system services.
	if backend := s.Controller.GetMonitoringBackend(); backend != "" {
		for _, svc := range []monitoring.SystemService{
			monitoring.NodeExporterSystemService(""),
			monitoring.PrometheusSystemService(),
			monitoring.MonitoringUISystemService(backend, s.Controller.GetNetworkControllerImage()),
		} {
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

	if uiMgr := s.Controller.GetUI(); uiMgr != nil {
		for _, svc := range uiMgr.SystemServices() {
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

// pullImage pulls a container image using podman.
func pullImage(ctx context.Context, image string) error {
	out, err := exec.CommandContext(ctx, "podman", "pull", image).CombinedOutput() //nolint:gosec // G204 -- image from system service config
	if err != nil {
		return fmt.Errorf("podman pull %s: %w: %s", image, err, string(out))
	}
	return nil
}

func (s *SystemControllerHandlers) refreshSystemServices(c *echo.Context) error {
	ctx := c.Request().Context()
	svcs := s.collectSystemServices()

	// Collect unique images.
	seen := map[string]struct{}{}
	var images []string
	for _, svc := range svcs {
		if _, ok := seen[svc.Image]; ok {
			continue
		}
		seen[svc.Image] = struct{}{}
		images = append(images, svc.Image)
	}

	// Pull all images in parallel (max 3 concurrent), collecting errors.
	var (
		pullMu     sync.Mutex
		pullErrors []string
		wg         sync.WaitGroup
		sem        = make(chan struct{}, 3)
	)
	for _, img := range images {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := pullImage(ctx, img); err != nil {
				pullMu.Lock()
				pullErrors = append(pullErrors, err.Error())
				pullMu.Unlock()
			}
		})
	}
	wg.Wait()

	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return echo.NewHTTPError(500, "systemd manager not available")
	}

	// Restart all system service units.
	for _, svc := range svcs {
		if err := sd.SetStatus(ctx, svc.UnitName, systemd.Restart); err != nil {
			pullErrors = append(pullErrors, fmt.Sprintf("restart %s: %v", svc.UnitName, err))
		}
	}

	// Find and restart all networkcontroller units.
	units, err := sd.ListUnits(ctx)
	if err == nil {
		for _, u := range units {
			if strings.HasSuffix(u.Name, "-network.service") && strings.HasPrefix(u.Name, systemd.PackageUnitPrefix) {
				if err := sd.SetStatus(ctx, u.Name, systemd.Restart); err != nil {
					pullErrors = append(pullErrors, fmt.Sprintf("restart %s: %v", u.Name, err))
				}
			}
		}
	}

	// Schedule systemcontroller restart in a goroutine with 1s delay.
	go func() {
		time.Sleep(1 * time.Second)
		if err := sd.SetStatus(s.ctx, "town-os-systemcontroller.service", systemd.Restart); err != nil {
			slog.Debug(fmt.Sprintf("restart systemcontroller: %v", err))
		}
	}()

	if len(pullErrors) > 0 {
		return c.JSON(200, map[string]string{"status": "ok", "errors": strings.Join(pullErrors, "; ")})
	}
	return c.JSON(200, map[string]string{"status": "ok"})
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
