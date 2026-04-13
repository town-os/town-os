package systemcontroller

import (
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// monitoringStatus returns the monitoring stack status by querying systemd
// unit states for the monitoring system services (prometheus, monitoring-ui,
// node-exporter). The browser talks to port 5308 directly (no proxy through
// the systemcontroller).
func (s *SystemControllerHandlers) monitoringStatus(c *echo.Context) error {
	backend := s.Controller.GetMonitoringBackend()
	if backend == "" {
		return c.JSON(200, map[string]string{"status": "disabled"})
	}

	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return c.JSON(200, map[string]string{"status": "disabled"})
	}

	unitStates := map[string]bool{}
	units, err := sd.ListUnits(c.Request().Context())
	if err == nil {
		for _, u := range units {
			unitStates[u.Name] = u.ActiveState == "active"
		}
	}

	monitoringUIActive := unitStates[systemd.SystemServiceUnitName("monitoring-ui")]
	status := monitoring.MonitoringStatus{
		Backend:      backend,
		Prometheus:   unitStates[systemd.SystemServiceUnitName("prometheus")],
		NodeExporter: unitStates[systemd.SystemServiceUnitName("node-exporter")],
		MonitoringUI: monitoringUIActive,
		DiskDevices:  s.Controller.GetDiskDevices(),
	}

	// In grafana mode, the monitoring-ui unit IS grafana.
	if backend == monitoring.BackendGrafana {
		status.Grafana = monitoringUIActive
	}

	return c.JSON(200, status)
}
