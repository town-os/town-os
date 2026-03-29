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

	status := monitoring.MonitoringStatus{
		Backend:      backend,
		Prometheus:   unitStates[systemd.SystemServiceUnitName("prometheus")],
		NodeExporter: unitStates[systemd.SystemServiceUnitName("node-exporter")],
	}

	// In grafana mode, the monitoring-ui unit IS grafana.
	if backend == monitoring.BackendGrafana {
		status.Grafana = unitStates[systemd.SystemServiceUnitName("monitoring-ui")]
	}

	return c.JSON(200, status)
}
