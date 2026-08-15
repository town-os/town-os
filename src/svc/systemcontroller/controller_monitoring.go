package systemcontroller

import (
	"context"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

// ScrapeTargetsFunc asks Prometheus which of its scrape jobs are answering.
//
// It is a function on the controller rather than a direct call so a test can
// answer without a Prometheus: the real implementation connects to a port in
// the HOST network namespace, which no unit test may depend on.
type ScrapeTargetsFunc func(ctx context.Context, ports monitoring.Ports) ([]monitoring.ScrapeTarget, error)

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

	// Prometheus's own target list, which is the only place a scrape job that
	// never worked is visible at all. Asked for only when Prometheus is active:
	// on a box where it is stopped the connection refusal is the unit state
	// already reported above, and repeating it as a scrape error would read as
	// a second, different fault.
	if status.Prometheus {
		fetch := s.Controller.GetScrapeTargetsFunc()
		if fetch == nil {
			fetch = func(ctx context.Context, ports monitoring.Ports) ([]monitoring.ScrapeTarget, error) {
				return monitoring.FetchScrapeTargets(ctx, nil, ports)
			}
		}
		targets, terr := fetch(c.Request().Context(), s.Controller.GetMonitoringPorts())
		if terr != nil {
			// Reported, not logged: an empty target list because nothing could
			// be asked must not look like an empty target list because nothing
			// is wrong.
			status.ScrapeTargetsError = terr.Error()
		} else {
			status.ScrapeTargets = targets
			status.DownJobs = monitoring.DownJobs(targets)
		}
	}

	// In grafana mode, the monitoring-ui unit IS grafana — but only once the
	// switch has actually swapped it. While the image is still downloading the
	// unit is the previous backend, active and answering :5308 with something
	// that is not Grafana, so reporting it here would put a Grafana iframe in
	// front of a socat forwarder.
	if backend == monitoring.BackendGrafana {
		status.Grafana = monitoringUIActive && !s.Controller.MonitoringUIPending()
	}

	return c.JSON(200, status)
}
