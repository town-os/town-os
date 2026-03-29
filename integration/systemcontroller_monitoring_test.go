// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
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

	monUnitName := systemd.UnitName(
		monitoring.MonitoringRepo,
		monitoring.MonitoringPackageName,
		monitoring.MonitoringVersion,
	)
	sd.Units = []systemd.UnitStatus{
		{Name: monUnitName, ActiveState: "active"},
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
