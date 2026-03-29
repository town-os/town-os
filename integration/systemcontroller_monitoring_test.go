// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

func TestMonitoringStatusDisabledByDefault(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	// When MonitoringBackend is empty, status returns {"status":"disabled"}.
	// The client decodes it as an empty MonitoringStatus (no backend).
	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != "" {
		t.Fatalf("expected empty backend, got %q", status.Backend)
	}
	if status.Prometheus {
		t.Fatal("expected prometheus not running")
	}
	if status.NodeExporter {
		t.Fatal("expected node-exporter not running")
	}
}

func TestMonitoringStatusUPlotBackend(t *testing.T) {
	t.Parallel()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	// All monitoring services are system services.
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != monitoring.BackendUPlot {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendUPlot, status.Backend)
	}
	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if !status.NodeExporter {
		t.Fatal("expected node-exporter running")
	}
}

func TestMonitoringNodeExporterRealStart(t *testing.T) {
	nePort := findFreePort(t)
	t.Logf("node-exporter port: %s", nePort)

	sd := systemd.NewManager()
	ctx := context.Background()

	if err := monitoring.StartNodeExporter(ctx, sd, nePort); err != nil {
		t.Fatalf("StartNodeExporter: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("node-exporter")
	t.Cleanup(func() {
		_ = sd.SetStatus(ctx, unitName, systemd.Stop) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(ctx, unitName)            //nolint:errcheck // best-effort cleanup
	})

	// Verify the unit was installed.
	units, err := sd.ListUnits(ctx)
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	found := false
	for _, u := range units {
		if u.Name == unitName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unit %s in unit list", unitName)
	}
}

func TestMonitoringSystemServiceUnitsUPlot(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	// Start Prometheus system service.
	if err := monitoring.StartPrometheus(t.Context(), sd, btrfsBase, ""); err != nil {
		t.Fatalf("StartPrometheus: %v", err)
	}

	// Start Monitoring UI (uplot) system service.
	if err := monitoring.StartMonitoringUI(t.Context(), sd, monitoring.BackendUPlot, "", ""); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	// Verify both units are system service units.
	promUnit := systemd.SystemServiceUnitName("prometheus")
	uiUnit := systemd.SystemServiceUnitName("monitoring-ui")

	promContent, ok := sd.InstalledUnits[promUnit]
	if !ok {
		t.Fatalf("expected prometheus unit %s to be installed", promUnit)
	}
	uiContent, ok := sd.InstalledUnits[uiUnit]
	if !ok {
		t.Fatalf("expected monitoring-ui unit %s to be installed", uiUnit)
	}

	// Both should be system service units.
	if !systemd.IsSystemServiceUnit(promUnit) {
		t.Fatalf("prometheus unit should be a system service unit")
	}
	if !systemd.IsSystemServiceUnit(uiUnit) {
		t.Fatalf("monitoring-ui unit should be a system service unit")
	}

	// Prometheus unit should have port 9090 mapping and config volume mount.
	if !strings.Contains(promContent, "9090:9090") {
		t.Fatalf("prometheus unit should map port 9090, got:\n%s", promContent)
	}
	if !strings.Contains(promContent, "prometheus-config:/etc/prometheus") {
		t.Fatalf("prometheus unit should mount config volume, got:\n%s", promContent)
	}
	if !strings.Contains(promContent, monitoring.PrometheusImage) {
		t.Fatalf("prometheus unit should reference prometheus image, got:\n%s", promContent)
	}

	// Monitoring UI unit should use socat forwarding 5308 to 9090.
	if !strings.Contains(uiContent, "socat") {
		t.Fatalf("uplot monitoring-ui unit should use socat, got:\n%s", uiContent)
	}
	if !strings.Contains(uiContent, "5308") {
		t.Fatalf("monitoring-ui unit should expose port 5308, got:\n%s", uiContent)
	}
}

func TestMonitoringSystemServiceUnitsGrafana(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	// Start Monitoring UI (grafana) system service.
	if err := monitoring.StartMonitoringUI(t.Context(), sd, monitoring.BackendGrafana, btrfsBase, ""); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	uiUnit := systemd.SystemServiceUnitName("monitoring-ui")
	uiContent, ok := sd.InstalledUnits[uiUnit]
	if !ok {
		t.Fatalf("expected monitoring-ui unit %s to be installed", uiUnit)
	}

	// In grafana mode, the unit should use grafana image and map 5308:3000.
	if !strings.Contains(uiContent, monitoring.GrafanaImage) {
		t.Fatalf("grafana monitoring-ui unit should use grafana image, got:\n%s", uiContent)
	}
	if !strings.Contains(uiContent, "5308:3000") {
		t.Fatalf("grafana monitoring-ui unit should map 5308:3000, got:\n%s", uiContent)
	}
}

func TestMonitoringPrometheusRealStart(t *testing.T) {
	sd := systemd.NewManager()
	ctx := context.Background()
	btrfsBase := t.TempDir()

	if err := monitoring.StartPrometheus(ctx, sd, btrfsBase, ""); err != nil {
		t.Fatalf("StartPrometheus: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("prometheus")
	t.Cleanup(func() {
		_ = sd.SetStatus(ctx, unitName, systemd.Stop) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(ctx, unitName)            //nolint:errcheck // best-effort cleanup
	})

	units, err := sd.ListUnits(ctx)
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	found := false
	for _, u := range units {
		if u.Name == unitName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unit %s in unit list", unitName)
	}
}

func TestMonitoringUIUPlotRealStart(t *testing.T) {
	sd := systemd.NewManager()
	ctx := context.Background()

	if err := monitoring.StartMonitoringUI(ctx, sd, monitoring.BackendUPlot, "", ""); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("monitoring-ui")
	t.Cleanup(func() {
		_ = sd.SetStatus(ctx, unitName, systemd.Stop) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(ctx, unitName)            //nolint:errcheck // best-effort cleanup
	})

	units, err := sd.ListUnits(ctx)
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	found := false
	for _, u := range units {
		if u.Name == unitName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unit %s in unit list", unitName)
	}
}
