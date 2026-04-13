package systemcontroller

import (
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// monitoringStatus returns the monitoring stack status by querying systemd
// unit states for the monitoring system services (prometheus, monitoring-ui,
// node-exporter). The browser talks to port 5308 directly (no proxy through
// the systemcontroller). Uses GetUnitStates so the status poll does not
// enumerate every unit file on disk, which is important on hosts with an
// overlayfs root where each disk stat triggers an inode lookup.
func (s *SystemControllerHandlers) monitoringStatus(c *echo.Context) error {
	backend := s.Controller.GetMonitoringBackend()
	if backend == "" {
		return c.JSON(200, map[string]string{"status": "disabled"})
	}

	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return c.JSON(200, map[string]string{"status": "disabled"})
	}

	prometheusUnit := systemd.SystemServiceUnitName("prometheus")
	nodeExporterUnit := systemd.SystemServiceUnitName("node-exporter")
	monitoringUIUnit := systemd.SystemServiceUnitName("monitoring-ui")

	unitStates := map[string]bool{}
	units, err := sd.GetUnitStates(c.Request().Context(), []string{
		prometheusUnit, nodeExporterUnit, monitoringUIUnit,
	})
	if err == nil {
		for _, u := range units {
			unitStates[u.Name] = u.ActiveState == "active"
		}
	}

	monitoringUIActive := unitStates[monitoringUIUnit]
	status := monitoring.MonitoringStatus{
		Backend:      backend,
		Prometheus:   unitStates[prometheusUnit],
		NodeExporter: unitStates[nodeExporterUnit],
		MonitoringUI: monitoringUIActive,
		DiskDevices:  s.Controller.GetDiskDevices(),
	}

	// In grafana mode, the monitoring-ui unit IS grafana.
	if backend == monitoring.BackendGrafana {
		status.Grafana = monitoringUIActive
	}

	return c.JSON(200, status)
}
