package systemcontroller

import (
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// monitoringStatus returns the monitoring stack status by querying systemd
// unit states for the monitoring package and node-exporter system service.
// The browser talks to port 5308 directly (no proxy through the systemcontroller).
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

	// The monitoring package runs as a regular package. Its service unit
	// name follows the standard pattern.
	pkgUnitName := systemd.UnitName(
		monitoring.MonitoringRepo,
		monitoring.MonitoringPackageName,
		monitoring.MonitoringVersion,
	)

	status := monitoring.MonitoringStatus{
		Backend:      backend,
		Prometheus:   unitStates[pkgUnitName],
		NodeExporter: unitStates[systemd.SystemServiceUnitName("node-exporter")],
	}

	// In grafana mode, the primary container is grafana and the package
	// unit represents grafana, not prometheus.
	if backend == monitoring.BackendGrafana {
		status.Grafana = unitStates[pkgUnitName]
		// Prometheus runs as a dependency.
		status.Prometheus = unitStates[systemd.UnitName(
			monitoring.MonitoringRepo,
			monitoring.MonitoringPackageName+"--dep--prometheus",
			monitoring.MonitoringVersion,
		)]
	}

	return c.JSON(200, status)
}
