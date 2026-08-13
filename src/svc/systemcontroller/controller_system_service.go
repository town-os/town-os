package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// RestartPendingMarkerFilename is the sentinel file the refresh handler
// writes into <btrfsBase> right before scheduling the systemcontroller's
// own self-restart. On the next boot the systemcontroller reads, acts
// on, and removes it. The act is: restart every installed package unit
// serially so the new systemcontroller inherits a fresh set of children.
const RestartPendingMarkerFilename = "town-os-restart-pending"

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

// SystemControllerServiceKey is the system service key used for the
// systemcontroller's own entry in the system services list. The UI and
// clients match on this key to identify the self entry (for example, to
// refuse the Stop action that would kill the very process serving the
// request).
const SystemControllerServiceKey = "systemcontroller"

// extractListenPort returns the port portion of a listen address like
// ":5309" or "0.0.0.0:5309". Returns an empty string when the input has
// no ":" separator.
func extractListenPort(listenAddr string) string {
	idx := strings.LastIndex(listenAddr, ":")
	if idx < 0 {
		return ""
	}
	return listenAddr[idx+1:]
}

// collectSystemServices gathers system service metadata from all providers.
func (s *SystemControllerHandlers) collectSystemServices() []systemServiceInfo {
	var all []systemServiceInfo

	// Monitoring services are all system services.
	if backend := s.Controller.GetMonitoringBackend(); backend != "" {
		ports := s.Controller.GetMonitoringPorts()
		for _, svc := range []monitoring.SystemService{
			monitoring.NodeExporterSystemService(ports),
			monitoring.PrometheusSystemService(ports),
			monitoring.MonitoringUISystemService(backend, s.Controller.GetNetworkControllerImage(), ports),
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

	// Ingress runs as a host systemd unit like the UI and rolodex; it must be
	// pulled and restarted on a system update too. Without this it was silently
	// excluded, so its image never advanced on "refresh system services".
	if ingMgr := s.Controller.GetIngress(); ingMgr != nil {
		for _, svc := range ingMgr.SystemServices() {
			all = append(all, systemServiceInfo{
				Key:         svc.Key,
				DisplayName: svc.DisplayName,
				Image:       svc.Image,
				Port:        svc.Port,
				UnitName:    svc.UnitName,
			})
		}
	}

	// One object-storage partition per network, each its own daemon and its own
	// unit. Listed for the same reason the ingress is: a service absent from
	// here is never re-pulled or restarted by "refresh system services", so its
	// image would stay on whatever it first started with forever.
	//
	// Sorted by key so the list does not reshuffle between polls — the registry
	// is a map, and a UI table that reordered itself every 30 seconds would be
	// unusable.
	if reg := s.Controller.GetGfehRegistry(); reg != nil {
		var partitions []systemServiceInfo
		for _, m := range reg.Managers() {
			for _, svc := range m.SystemServices() {
				partitions = append(partitions, systemServiceInfo{
					Key:         svc.Key,
					DisplayName: svc.DisplayName,
					Image:       svc.Image,
					Port:        svc.Port,
					UnitName:    svc.UnitName,
				})
			}
		}
		sort.Slice(partitions, func(i, j int) bool { return partitions[i].Key < partitions[j].Key })
		all = append(all, partitions...)
	}

	// The systemcontroller itself runs as a host systemd unit. List it as
	// a system service so users can see its status and trigger restarts
	// from the UI. The image and listen address are plumbed through
	// ServerConfig from main.go; tests that don't configure them omit the
	// entry entirely.
	if img := s.Controller.GetSystemControllerImage(); img != "" {
		all = append(all, systemServiceInfo{
			Key:         SystemControllerServiceKey,
			DisplayName: "System Controller",
			Image:       img,
			Port:        extractListenPort(s.Controller.GetSystemControllerListenAddr()),
			UnitName:    systemd.SystemControllerUnitName,
		})
	}

	return all
}

func (s *SystemControllerHandlers) listSystemServices(c *echo.Context) error {
	svcs := s.collectSystemServices()
	if len(svcs) == 0 {
		return c.JSON(200, []SystemServiceEntry{})
	}

	sd := s.Controller.GetSystemdManager()

	// Build a set of unit names we care about, then look them up.
	targetUnits := make(map[string]struct{}, len(svcs))
	for _, svc := range svcs {
		targetUnits[svc.UnitName] = struct{}{}
	}

	unitStates := map[string]systemd.UnitStatus{}
	if sd != nil {
		units, err := sd.ListUnits(c.Request().Context())
		if err == nil {
			for _, u := range units {
				if _, ok := targetUnits[u.Name]; ok {
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

// pullImage pulls a container image using podman. CONTAINER_HOST is
// set at systemcontroller startup so the pull hits the host's podman
// socket instead of the systemcontroller container's isolated storage.
//
// Declared as a package-level variable so integration tests can
// substitute a no-op implementation and drive refreshSystemServices
// without needing a live podman daemon in the test environment.
var pullImage = func(ctx context.Context, image string) error {
	out, err := exec.CommandContext(ctx, "podman", "pull", image).CombinedOutput() //nolint:gosec // G204 -- image from system service config
	if err != nil {
		return fmt.Errorf("podman pull %s: %w: %s", image, err, string(out))
	}
	return nil
}

// autoUpdateDisabled reports whether a SCHEDULED refresh should be skipped.
//
// Only the timer's refresh consults this. An operator who presses the update
// button is making an explicit request, and a setting named "update
// automatically" has nothing to say about it — disabling the daily update must
// not disable the ability to update.
//
// Unreadable settings fall through to enabled, which is the safe direction: the
// installer ships only the systemcontroller and rolodex images, so a box that
// stops pulling is a box missing most of its services. A broken settings read
// should not quietly strand one there.
func (s *SystemControllerHandlers) autoUpdateDisabled(ctx context.Context) bool {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return false
	}
	v, err := mgr.Get(ctx, account.AutoUpdateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto-update setting read: %v\n", err)
		return false
	}
	return autoUpdateDisabledValue(v)
}

func (s *SystemControllerHandlers) refreshSystemServices(c *echo.Context) error {
	ctx := c.Request().Context()

	// The daily timer marks its own call so the setting can gate it without
	// gating the operator (see autoUpdateDisabled). 200 rather than an error:
	// the timer asked whether to update and got a valid answer of "not now",
	// which is not a failure for `systemctl status` to show in red.
	if c.Request().URL.Query().Get(ScheduledRefreshQuery) != "" && s.autoUpdateDisabled(ctx) {
		return c.JSON(200, map[string]string{
			"status": "skipped",
			"reason": account.AutoUpdateKey + " is disabled",
		})
	}

	svcs := s.collectSystemServices()

	// Pull every system-service image, in dependency order: the systemcontroller
	// (the version anchor — pulled first so the freshly-pulled image is already
	// local when it self-restarts at the end), then rolodex (the box's DNS, which
	// the other images may need to resolve their registry), then everything else.
	scImage := s.Controller.GetSystemControllerImage()
	rolImages := make(map[string]struct{})
	if rol := s.Controller.GetRolodex(); rol != nil {
		for _, svc := range rol.SystemServices() {
			rolImages[svc.Image] = struct{}{}
		}
	}

	seen := make(map[string]struct{}, len(svcs))
	var scOrder, rolOrder, otherOrder []string
	for _, svc := range svcs {
		if svc.Image == "" {
			continue
		}
		if _, ok := seen[svc.Image]; ok {
			continue
		}
		seen[svc.Image] = struct{}{}
		_, isRolodex := rolImages[svc.Image]
		switch {
		case svc.Image == scImage:
			scOrder = append(scOrder, svc.Image)
		case isRolodex:
			rolOrder = append(rolOrder, svc.Image)
		default:
			otherOrder = append(otherOrder, svc.Image)
		}
	}

	var (
		pullMu     sync.Mutex
		pullErrors []string
	)
	pullOne := func(img string) {
		if err := pullImage(ctx, img); err != nil {
			pullMu.Lock()
			pullErrors = append(pullErrors, err.Error())
			pullMu.Unlock()
		}
	}

	// Anchor + DNS first, sequentially, so the ordering is guaranteed.
	for _, img := range scOrder {
		pullOne(img)
	}
	for _, img := range rolOrder {
		pullOne(img)
	}
	// The remaining images have no ordering constraint; pull them in parallel
	// (max 3 concurrent).
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, 3)
	)
	for _, img := range otherOrder {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			pullOne(img)
		})
	}
	wg.Wait()

	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return echo.NewHTTPError(500, "systemd manager not available")
	}

	// Restart all system service units. Skip the systemcontroller's own
	// unit — synchronously restarting it here would kill the process
	// serving this HTTP request before we can return a response. A
	// delayed goroutine below schedules the self-restart instead.
	for _, svc := range svcs {
		if svc.UnitName == systemd.SystemControllerUnitName {
			continue
		}
		if err := sd.SetStatus(ctx, svc.UnitName, systemd.Restart); err != nil {
			pullErrors = append(pullErrors, fmt.Sprintf("restart %s: %v", svc.UnitName, err))
		}
	}

	// Find and restart all networkcontroller units (both package and system service).
	units, err := sd.ListUnits(ctx)
	if err == nil {
		for _, u := range units {
			if strings.HasSuffix(u.Name, "-network.service") &&
				(strings.HasPrefix(u.Name, systemd.PackageUnitPrefix) || strings.HasPrefix(u.Name, systemd.SystemServiceUnitPrefix)) {
				if err := sd.SetStatus(ctx, u.Name, systemd.Restart); err != nil {
					pullErrors = append(pullErrors, fmt.Sprintf("restart %s: %v", u.Name, err))
				}
			}
		}
	}

	// Drop the freshness marker BEFORE scheduling self-restart. On the
	// next boot RunFreshnessStage reads this marker and restarts every
	// installed package serially so the new process inherits a freshly
	// started child set. Writing after the delayed restart would race
	// the restart — the old process could exit with the file not yet
	// on disk.
	if err := WriteRestartPendingMarker(s.Controller.GetBtrfsBasePath()); err != nil {
		// Non-fatal: the refresh still happens, just without the
		// freshness restart on next boot.
		slog.Warn("write restart-pending marker",
			slog.String("err", err.Error()))
	}

	// Schedule systemcontroller restart in a goroutine with 1s delay.
	go func() {
		time.Sleep(1 * time.Second)
		if err := sd.SetStatus(s.ctx, systemd.SystemControllerUnitName, systemd.Restart); err != nil {
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

	// Refuse to stop the systemcontroller from its own HTTP handler —
	// that would kill the process serving this very request. Restart is
	// still permitted (systemd respawns us).
	if req.Key == SystemControllerServiceKey && req.Action == systemd.Stop {
		return echo.NewHTTPError(400, "cannot stop the system controller from its own API")
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

	// Restarting the systemcontroller from its own HTTP handler would
	// cut off the response mid-flight when systemd kills the old process.
	// Schedule the restart on a short delay so the handler can return
	// 200 first; systemd respawns us after the delay fires.
	if req.Key == SystemControllerServiceKey && req.Action == systemd.Restart {
		go func() {
			time.Sleep(1 * time.Second)
			if err := sd.SetStatus(s.ctx, unitName, systemd.Restart); err != nil {
				slog.Debug(fmt.Sprintf("delayed restart %s: %v", unitName, err))
			}
		}()
		return c.JSON(200, map[string]string{"status": "ok"})
	}

	if err := sd.SetStatus(c.Request().Context(), unitName, req.Action); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("set status: %v", err))
	}

	return c.JSON(200, map[string]string{"status": "ok"})
}
